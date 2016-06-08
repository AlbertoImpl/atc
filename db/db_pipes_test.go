package db_test

import (
	"time"

	"github.com/lib/pq"
	"github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
)

var _ = Describe("Pipes", func() {
	var dbConn db.Conn
	var listener *pq.Listener

	var database db.DB
	var teamDB db.TeamDB
	var buildDBFactory db.BuildDBFactory

	BeforeEach(func() {
		postgresRunner.Truncate()

		dbConn = db.Wrap(postgresRunner.Open())
		listener = pq.NewListener(postgresRunner.DataSourceName(), time.Second, time.Minute, nil)

		Eventually(listener.Ping, 5*time.Second).ShouldNot(HaveOccurred())
		bus := db.NewNotificationsBus(listener, dbConn)

		database = db.NewSQL(dbConn, bus)

		teamDBFactory := db.NewTeamDBFactory(dbConn)
		teamDB = teamDBFactory.GetTeamDB(atc.DefaultTeamName)

		buildDBFactory = db.NewBuildDBFactory(dbConn, bus)
	})

	AfterEach(func() {
		err := dbConn.Close()
		Expect(err).NotTo(HaveOccurred())

		err = listener.Close()
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("CreatePipe", func() {
		It("saves a pipe to the db", func() {
			myGuid, err := uuid.NewV4()
			Expect(err).NotTo(HaveOccurred())

			err = database.CreatePipe(myGuid.String(), "a-url")
			Expect(err).NotTo(HaveOccurred())

			pipe, err := database.GetPipe(myGuid.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(pipe.ID).To(Equal(myGuid.String()))
			Expect(pipe.URL).To(Equal("a-url"))
		})
	})
})
