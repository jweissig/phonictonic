package main

import "github.com/jinzhu/gorm"

// Task -
type Task struct {
	gorm.Model
	JobUUID         string
	TaskUUID        string
	Status          string
	Filename        string
	FormatName      string
	FileSize        int64
	DurationSeconds float64
	JSONRaw         string
}

// TableName -- this is where we store the tasks for each job
func (n Task) TableName() string {
	return "tasks"
}
