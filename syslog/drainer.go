package syslog

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/concourse/atc/db"
	"github.com/concourse/atc/event"
	sl "github.com/papertrail/remote_syslog2/syslog"
)

const ServerPollingInterval = 5 * time.Second

//go:generate counterfeiter . Drainer

type Drainer interface {
	Run(context.Context) error
}

type drainer struct {
	hostname  string
	transport string `yaml:"transport"`
	address   string `yaml:"address"`

	buildFactory db.BuildFactory
}

func NewDrainer(transport string, address string, hostname string, buildFactory db.BuildFactory) Drainer {
	return &drainer{
		hostname:     hostname,
		transport:    transport,
		address:      address,
		buildFactory: buildFactory,
	}
}

func (d *drainer) Run(ctx context.Context) error {
	// logger := lagerctx.FromContext(ctx).Session("syslog-drain")

	builds, err := d.buildFactory.GetDrainableBuilds()
	if err != nil {
		return err
	}

	syslog, err := sl.Dial(
		d.hostname,
		d.transport,
		d.address,
		nil,
		30*time.Second,
		30*time.Second,
		99990,
	)

	if err != nil {
		return err
	}

	for _, build := range builds {
		events, err := build.Events(0)
		if err != nil {
			return err
		}

		for {
			ev, err := events.Next()
			if err != nil {
				if err == db.ErrEndOfBuildEventStream {
					break
				}
				return err
			}

			if ev.Event == "log" {
				var log event.Log
				err := json.Unmarshal(*ev.Data, &log)
				if err != nil {
					return err
				}

				syslog.Packets <- sl.Packet{
					Severity: sl.SevInfo,
					Facility: sl.LogUser,
					Hostname: d.hostname,
					Tag:      "build#" + strconv.Itoa(build.ID()),
					Time:     time.Unix(log.Time, 0),
					Message:  log.Payload,
				}

				select {
				case err := <-syslog.Errors:
					return err
				default:
					continue
				}
			}
		}

		err = build.SetDrained(true)
		if err != nil {
			return err
		}
	}

	return nil
}
