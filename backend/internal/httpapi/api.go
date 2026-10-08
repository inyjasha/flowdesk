package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flowdesk/internal/auth"
	"flowdesk/internal/domain"
	"flowdesk/internal/events"
	"flowdesk/internal/security"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type Repository interface {
	Session(context.Context, string) (domain.User, error)
	Logout(context.Context, string) error
	Workspaces(context.Context, int64) ([]domain.Workspace, error)
	CreateWorkspace(context.Context, int64, string) (domain.Workspace, error)
	Role(context.Context, int64, int64) (string, error)
	Members(context.Context, int64, int64) ([]domain.Member, error)
	Invite(context.Context, int64, int64, string) (string, error)
	Join(context.Context, int64, string) (domain.Workspace, error)
	Tasks(context.Context, int64, int64, string, int, int, string, *int64) ([]domain.Task, int, error)
	SaveTask(context.Context, int64, int64, int64, domain.TaskInput) (domain.Task, error)
	DeleteTask(context.Context, int64, int64, int64, int) error
	Activities(context.Context, int64, int64) ([]domain.Activity, error)
}
type API struct {
	Store        Repository
	Auth         *auth.Service
	Redis        *redis.Client
	Origin       string
	SecureCookie bool
	StaticDir    string
	Ready        func(context.Context) error
	Shutdown     context.Context
}

func (a *API) Router() *gin.Engine {
	r := gin.New()
	r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		started := time.Now()
		id, _ := security.Token()
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
		if !strings.HasSuffix(c.Request.URL.Path, "/stream") {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
		slog.Info("HTTP", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration", time.Since(started).String(), "request_id", id)
	}, func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				fail(c, errors.New("panic в обработчике"))
			}
		}()
		c.Next()
	})
	r.GET("/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/health/ready", func(c *gin.Context) {
		if err := a.Ready(c.Request.Context()); err != nil {
			c.JSON(503, gin.H{"error": "зависимости недоступны"})
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	api := r.Group("/api", a.csrf)
	api.POST("/auth/register", a.authLimit, a.register)
	api.POST("/auth/login", a.authLimit, a.login)
	private := api.Group("", a.session)
	private.GET("/me", func(c *gin.Context) { c.JSON(200, user(c)) })
	private.POST("/auth/logout", a.logout)
	private.GET("/workspaces", a.workspaces)
	private.POST("/workspaces", a.createWorkspace)
	private.POST("/invites/accept", a.join)
	workspace := private.Group("/workspaces/:workspace", a.workspaceID)
	workspace.GET("/members", a.members)
	workspace.POST("/invites", a.invite)
	workspace.GET("/tasks", a.tasks)
	workspace.POST("/tasks", a.saveTask)
	workspace.PUT("/tasks/:task", a.taskID, a.saveTask)
	workspace.DELETE("/tasks/:task", a.taskID, a.deleteTask)
	workspace.GET("/activity", a.activity)
	workspace.GET("/stream", a.stream)
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != "GET" || strings.HasPrefix(c.Request.URL.Path, "/api") || a.StaticDir == "" {
			c.JSON(404, gin.H{"error": "маршрут не найден"})
			return
		}
		path := filepath.Join(a.StaticDir, filepath.FromSlash(filepath.Clean("/"+c.Request.URL.Path)))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			c.File(path)
			return
		}
		c.File(filepath.Join(a.StaticDir, "index.html"))
	})
	return r
}

func fail(c *gin.Context, err error) {
	code := 500
	message := "внутренняя ошибка сервера"
	switch {
	case errors.Is(err, domain.ErrInvalid):
		code = 400
		message = err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		code = 401
		message = "неверные данные входа или сессия истекла"
	case errors.Is(err, domain.ErrForbidden):
		code = 403
		message = err.Error()
	case errors.Is(err, domain.ErrNotFound):
		code = 404
		message = err.Error()
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrEmailTaken):
		code = 409
		message = err.Error()
	default:
		slog.Error("ошибка API", "error", err, "request_id", c.GetString("request_id"))
	}
	c.AbortWithStatusJSON(code, gin.H{"error": message, "request_id": c.GetString("request_id")})
}
func user(c *gin.Context) domain.User { return c.MustGet("user").(domain.User) }
func wid(c *gin.Context) int64        { return c.MustGet("workspace_id").(int64) }
func (a *API) csrf(c *gin.Context) {
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
		origin := c.GetHeader("Origin")
		if c.GetHeader("X-Flowdesk-Request") != "1" || (origin != "" && origin != a.Origin) {
			fail(c, domain.ErrForbidden)
			return
		}
	}
	c.Next()
}
func (a *API) session(c *gin.Context) {
	token, err := c.Cookie("flowdesk_session")
	if err != nil || len(token) != 64 {
		fail(c, domain.ErrUnauthorized)
		return
	}
	lookup, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	u, err := a.Store.Session(lookup, token)
	if err != nil {
		fail(c, err)
		return
	}
	c.Set("user", u)
	c.Next()
}
func (a *API) cookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("flowdesk_session", token, maxAge, "/api", "", a.SecureCookie, true)
}
func (a *API) register(c *gin.Context) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	u, token, err := a.Auth.Register(c.Request.Context(), in.Name, in.Email, in.Password)
	if err != nil {
		fail(c, err)
		return
	}
	a.cookie(c, token, 604800)
	c.JSON(201, u)
}
func (a *API) login(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	u, token, err := a.Auth.Login(c.Request.Context(), in.Email, in.Password)
	if err != nil {
		fail(c, err)
		return
	}
	a.cookie(c, token, 604800)
	c.JSON(200, u)
}
func (a *API) logout(c *gin.Context) {
	token, _ := c.Cookie("flowdesk_session")
	if err := a.Store.Logout(c.Request.Context(), token); err != nil {
		fail(c, err)
		return
	}
	a.cookie(c, "", -1)
	c.Status(204)
}
func (a *API) workspaceID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("workspace"), 10, 64)
	if err != nil || id < 1 {
		fail(c, domain.ErrInvalid)
		return
	}
	c.Set("workspace_id", id)
	c.Next()
}
func (a *API) taskID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("task"), 10, 64)
	if err != nil || id < 1 {
		fail(c, domain.ErrInvalid)
		return
	}
	c.Set("task_id", id)
	c.Next()
}
func (a *API) workspaces(c *gin.Context) {
	items, err := a.Store.Workspaces(c.Request.Context(), user(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, items)
}
func (a *API) createWorkspace(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	w, err := a.Store.CreateWorkspace(c.Request.Context(), user(c).ID, in.Name)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, w)
}
func (a *API) members(c *gin.Context) {
	items, err := a.Store.Members(c.Request.Context(), wid(c), user(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, items)
}
func (a *API) invite(c *gin.Context) {
	var in struct {
		Role string `json:"role"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	token, err := a.Store.Invite(c.Request.Context(), wid(c), user(c).ID, in.Role)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, gin.H{"token": token})
}
func (a *API) join(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.Token) != 64 {
		fail(c, domain.ErrInvalid)
		return
	}
	w, err := a.Store.Join(c.Request.Context(), user(c).ID, in.Token)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, w)
}
func (a *API) tasks(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	var assignee *int64
	if raw := c.Query("assignee"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || id < 1 {
			fail(c, domain.ErrInvalid)
			return
		}
		assignee = &id
	}
	items, total, err := a.Store.Tasks(c.Request.Context(), wid(c), user(c).ID, c.Query("q"), limit, offset, c.Query("priority"), assignee)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}
func (a *API) saveTask(c *gin.Context) {
	var in domain.TaskInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	var id int64
	if value, ok := c.Get("task_id"); ok {
		id = value.(int64)
	}
	task, err := a.Store.SaveTask(c.Request.Context(), wid(c), user(c).ID, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	code := 200
	if id == 0 {
		code = 201
	}
	c.JSON(code, task)
}
func (a *API) deleteTask(c *gin.Context) {
	version, err := strconv.Atoi(c.Query("version"))
	if err != nil {
		fail(c, domain.ErrInvalid)
		return
	}
	err = a.Store.DeleteTask(c.Request.Context(), wid(c), user(c).ID, c.MustGet("task_id").(int64), version)
	if err != nil {
		fail(c, err)
		return
	}
	c.Status(204)
}
func (a *API) activity(c *gin.Context) {
	items, err := a.Store.Activities(c.Request.Context(), wid(c), user(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, items)
}

var limitScript = redis.NewScript(`local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; return n`)

func (a *API) authLimit(c *gin.Context) {
	n, err := limitScript.Run(c.Request.Context(), a.Redis, []string{"auth:" + c.ClientIP()}, 60).Int64()
	if err != nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "вход временно недоступен"})
		return
	}
	if n > 15 {
		c.Header("Retry-After", "60")
		c.AbortWithStatusJSON(429, gin.H{"error": "слишком много попыток входа, подождите минуту"})
		return
	}
	c.Next()
}

func (a *API) stream(c *gin.Context) {
	lookup, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	_, err := a.Store.Role(lookup, wid(c), user(c).ID)
	cancel()
	if err != nil {
		fail(c, err)
		return
	}
	ctx := c.Request.Context()
	sub := a.Redis.Subscribe(ctx, events.Channel(wid(c)))
	defer sub.Close()
	handshake, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = sub.Receive(handshake)
	cancel()
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	controller := http.NewResponseController(c.Writer)
	send := func(kind, data string) error {
		controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", kind, data); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
	if send("ready", "{}") != nil {
		return
	}
	messages := sub.Channel()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// Соединение ограничено по времени: при переподключении снова проверяется сессия и перечитывается доска.
	lifetime := time.NewTimer(5 * time.Minute)
	defer lifetime.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.Shutdown.Done():
			return
		case <-lifetime.C:
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			if send("change", msg.Payload) != nil {
				return
			}
		case <-heartbeat.C:
			check, cancel := context.WithTimeout(ctx, 3*time.Second)
			token, _ := c.Cookie("flowdesk_session")
			_, err := a.Store.Session(check, token)
			cancel()
			if err != nil {
				return
			}
			if send("ping", "{}") != nil {
				return
			}
		}
	}
}
