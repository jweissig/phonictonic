package main

import "github.com/jinzhu/gorm"

// Job -
type Job struct {
	gorm.Model
	JobUUID                   string
	Email                     string
	JobCost                   string `sql:"-"` // not in data model (counting tasks)
	TasksCount                int    `sql:"-"` // not in data model (counting tasks)
	TasksTotalFilesize        string `sql:"-"` // not in data model (counting tasks)
	TasksTotalDurationSeconds string `sql:"-"` // not in data model (counting tasks)
}

// Task -
type Task struct {
	gorm.Model
	JobUUID               string
	TaskUUID              string
	Status                string
	Filename              string
	FormatName            string
	FileSize              int64
	DurationSeconds       float64
	JSONRaw               string
	TaskCost              string `sql:"-"` // not in data model (counting tasks)
	DurationSecondsString string `sql:"-"` // not in data model (counting tasks)
	FilesizeString        string `sql:"-"` // not in data model (counting tasks)
}

// used to parse json response from speech-to-text api

// Response -
type Response struct {
	Results []Result `json:"results"`
}

// Result -
type Result struct {
	Alternatives []Alternative `json:"alternatives"`
}

// Alternative -
type Alternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
	Words      []Word  `json:"words"`
}

// Word -
type Word struct {
	StartTime  string  `json:"startTime"`
	EndTime    string  `json:"endTime"`
	Word       string  `json:"word"`
	Confidence float64 `json:"confidence"`
	SpeakerTag int     `json:"speakerTag"`
}

// TableName -- this is where we store the jobs (with many tasks)
func (n Job) TableName() string {
	return "jobs"
}

// getJob - get job details
func getJob(jobID string) (response Job, err error) {

	var job Job
	db.Table("jobs").Select("jobs.id, jobs.job_uuid, jobs.email, jobs.created_at, jobs.updated_at, jobs.deleted_at, round(sum(tasks.duration_seconds/60*0.036),2) as job_cost, count(*) as tasks_count, sys.format_bytes(sum(tasks.file_size)) as tasks_total_filesize, TIME_FORMAT(SEC_TO_TIME(sum(tasks.duration_seconds)),'%Hh %im') as tasks_total_duration_seconds").Joins("INNER JOIN tasks ON jobs.job_uuid = tasks.job_uuid").Where("jobs.job_uuid = ?", jobID).Group("jobs.job_uuid, jobs.email, jobs.id").Find(&job)
	return job, err

}

// getAllJobs - list all jobs
func getAllJobs() (response []Job, err error) {

	var jobs []Job
	db.Table("jobs, tasks").Select("jobs.id, jobs.job_uuid, jobs.email, jobs.created_at, jobs.updated_at, jobs.deleted_at, round(sum(tasks.duration_seconds/60*0.036),2) as job_cost, count(*) as tasks_count, sys.format_bytes(sum(tasks.file_size)) as tasks_total_filesize, TIME_FORMAT(SEC_TO_TIME(sum(tasks.duration_seconds)),'%Hh %im') as tasks_total_duration_seconds").Where("jobs.job_uuid = tasks.job_uuid").Group("jobs.job_uuid, jobs.email, jobs.id").Order("jobs.created_at DESC").Scan(&jobs)
	return jobs, err

}

// TableName -- this is where we store the tasks for each job
func (n Task) TableName() string {
	return "tasks"
}

// getTasks - list all tasks for a job
func getTasks(jobID string) (response []Task, err error) {

	var tasks []Task
	db.Table("tasks").Select("id, job_uuid, task_uuid, status, filename, file_size, sys.format_bytes(file_size) as filesize_string, format_name, duration_seconds, TIME_FORMAT(SEC_TO_TIME(duration_seconds),'%Hh %im') as duration_seconds_string, round(duration_seconds/60*0.036,2) as task_cost, created_at, updated_at, deleted_at").Where("job_uuid = ?", jobID).Order("created_at DESC").Scan(&tasks)
	return tasks, err

}

// getTaskByID - list task for a job by file
func getTaskByID(jobID string, taskID string) (response Task, err error) {

	var task Task
	db.Table("tasks").Select("id, job_uuid, task_uuid, status, filename, file_size, sys.format_bytes(file_size) as filesize_string, format_name, duration_seconds, TIME_FORMAT(SEC_TO_TIME(duration_seconds),'%Hh %im') as duration_seconds_string, json_raw, round(duration_seconds/60*0.036,2) as task_cost, created_at, updated_at, deleted_at").Where("job_uuid = ? and task_uuid = ?", jobID, taskID).Find(&task)
	return task, err

}

// getTasksCount - return count of all tasks for a job
func getTasksCount(jobID string) (response int) {

	var count int
	db.Table("tasks").Where("job_uuid = ? AND deleted_at IS NULL", jobID).Count(&count)
	return count

}
