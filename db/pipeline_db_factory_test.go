package db_test

import (
	"time"

	"github.com/concourse/atc/db"
	"github.com/concourse/atc/db/fakes"
	"github.com/lib/pq"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PipelineDBFactory", func() {
	var dbConn db.Conn
	var listener *pq.Listener

	var pipelineDBFactory db.PipelineDBFactory

	var pipelinesDB *fakes.FakePipelinesDB

	BeforeEach(func() {
		postgresRunner.Truncate()

		dbConn = db.Wrap(postgresRunner.Open())

		listener = pq.NewListener(postgresRunner.DataSourceName(), time.Second, time.Minute, nil)
		Eventually(listener.Ping, 5*time.Second).ShouldNot(HaveOccurred())
		bus := db.NewNotificationsBus(listener, dbConn)

		pipelinesDB = new(fakes.FakePipelinesDB)

		pipelineDBFactory = db.NewPipelineDBFactory(lagertest.NewTestLogger("test"), dbConn, bus, pipelinesDB)
	})

	AfterEach(func() {
		err := dbConn.Close()
		Expect(err).NotTo(HaveOccurred())

		err = listener.Close()
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("default pipeline", func() {
		It("is the first one returned from the DB", func() {
			savedPipelineOne := db.SavedPipeline{
				ID: 1,
				Pipeline: db.Pipeline{
					Name: "a-pipeline",
				},
			}

			savedPipelineTwo := db.SavedPipeline{
				ID: 2,
				Pipeline: db.Pipeline{
					Name: "another-pipeline",
				},
			}

			pipelinesDB.GetAllActivePipelinesReturns([]db.SavedPipeline{
				savedPipelineOne,
				savedPipelineTwo,
			}, nil)

			defaultPipelineDB, found, err := pipelineDBFactory.BuildDefault()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			Expect(defaultPipelineDB.GetPipelineName()).To(Equal("a-pipeline"))
		})

		Context("when there are no pipelines", func() {
			BeforeEach(func() {
				pipelinesDB.GetAllActivePipelinesReturns([]db.SavedPipeline{}, nil)
			})

			It("returns a useful error if there are no pipelines", func() {
				_, found, err := pipelineDBFactory.BuildDefault()
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())
			})
		})
	})
})
