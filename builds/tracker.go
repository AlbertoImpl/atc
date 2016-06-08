package builds

import (
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/engine"
	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter . TrackerDB

type TrackerDB interface {
	GetAllStartedBuilds() ([]db.Build, error)
}

func NewTracker(
	logger lager.Logger,

	trackerDB TrackerDB,
	buildDBFactory db.BuildDBFactory,
	engine engine.Engine,
) *Tracker {
	return &Tracker{
		logger:         logger,
		trackerDB:      trackerDB,
		buildDBFactory: buildDBFactory,
		engine:         engine,
	}
}

type Tracker struct {
	logger lager.Logger

	trackerDB      TrackerDB
	buildDBFactory db.BuildDBFactory
	engine         engine.Engine
}

func (bt *Tracker) Track() {
	bt.logger.Debug("start")
	defer bt.logger.Debug("done")
	builds, err := bt.trackerDB.GetAllStartedBuilds()
	if err != nil {
		bt.logger.Error("failed-to-lookup-started-builds", err)
	}

	for _, b := range builds {
		tLog := bt.logger.Session("track", lager.Data{
			"build": b.ID,
		})

		buildDB := bt.buildDBFactory.GetBuildDB(b)
		engineBuild, err := bt.engine.LookupBuild(tLog, buildDB)
		if err != nil {
			tLog.Error("failed-to-lookup-build", err)

			err := buildDB.MarkAsFailed(err)
			if err != nil {
				tLog.Error("failed-to-mark-build-as-errored", err)
			}

			continue
		}

		go engineBuild.Resume(tLog)
	}
}
