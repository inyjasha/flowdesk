package domain

import "testing"

func TestTaskValidation(t *testing.T) {
	good := TaskInput{Title: " Первая задача ", Status: "todo", Priority: "medium"}
	if err := good.Validate(); err != nil || good.Title != "Первая задача" {
		t.Fatalf("%+v: %v", good, err)
	}
	for _, input := range []TaskInput{
		{Title: " ", Status: "todo", Priority: "low"},
		{Title: "Task", Status: "unknown", Priority: "low"},
		{Title: "Task", Status: "todo", Priority: "urgent"},
	} {
		if input.Validate() == nil {
			t.Fatal("неверная задача принята", input)
		}
	}
}
func TestRoles(t *testing.T) {
	if !CanEdit("owner") || !CanEdit("editor") || CanEdit("viewer") || CanEdit("") {
		t.Fatal("неверные права редактирования")
	}
}
