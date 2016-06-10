package db

import "github.com/concourse/atc"

type Job struct {
	Name string
}

type SavedJob struct {
	ID                 int
	Paused             bool
	PipelineName       string
	FirstLoggedBuildID int
	TeamID             int
	Job
}

type DashboardJob struct {
	Job       SavedJob
	JobConfig atc.JobConfig

	FinishedBuild BuildDB
	NextBuild     BuildDB
}

type Dashboard []DashboardJob
