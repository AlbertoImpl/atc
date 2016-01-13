package resource_test

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/resource/fakes"
	"github.com/concourse/atc/worker"
	wfakes "github.com/concourse/atc/worker/fakes"
	"github.com/concourse/baggageclaim"
	bfakes "github.com/concourse/baggageclaim/fakes"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/concourse/atc/resource"
)

type testMetadata []string

func (m testMetadata) Env() []string { return m }

var _ = Describe("Tracker", func() {
	var (
		fakeDB  *fakes.FakeTrackerDB
		tracker Tracker
	)

	var session = Session{
		ID: worker.Identifier{
			WorkerName: "some-worker",
		},
		Metadata: worker.Metadata{
			EnvironmentVariables: []string{"some=value"},
		},
		Ephemeral: true,
	}

	BeforeEach(func() {
		fakeDB = new(fakes.FakeTrackerDB)
		tracker = NewTracker(workerClient, fakeDB)
	})

	Describe("Init", func() {
		var (
			logger   *lagertest.TestLogger
			metadata Metadata = testMetadata{"a=1", "b=2"}

			initType ResourceType

			initResource Resource
			initErr      error
		)

		BeforeEach(func() {
			logger = lagertest.NewTestLogger("test")
			initType = "type1"

			workerClient.CreateContainerReturns(fakeContainer, nil)
		})

		JustBeforeEach(func() {
			initResource, initErr = tracker.Init(logger, metadata, session, initType, []string{"resource", "tags"})
		})

		Context("when a container does not exist for the session", func() {
			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, nil)
			})

			It("does not error and returns a resource", func() {
				Expect(initErr).NotTo(HaveOccurred())
				Expect(initResource).NotTo(BeNil())
			})

			It("creates a container with the resource's type, env, ephemeral information, and the session as the handle", func() {
				_, id, containerMetadata, spec := workerClient.CreateContainerArgsForCall(0)

				Expect(id).To(Equal(session.ID))
				Expect(containerMetadata).To(Equal(session.Metadata))
				resourceSpec := spec.(worker.ResourceTypeContainerSpec)

				Expect(resourceSpec.Type).To(Equal(string(initType)))
				Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
				Expect(resourceSpec.Ephemeral).To(Equal(true))
				Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
				Expect(resourceSpec.Cache).To(BeZero())
			})

			Context("when creating the container fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					workerClient.CreateContainerReturns(nil, disaster)
				})

				It("returns the error and no resource", func() {
					Expect(initErr).To(Equal(disaster))
					Expect(initResource).To(BeNil())
				})
			})
		})

		Context("when looking up the container fails for some reason", func() {
			disaster := errors.New("nope")

			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, disaster)
			})

			It("returns the error and no resource", func() {
				Expect(initErr).To(Equal(disaster))
				Expect(initResource).To(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})
		})

		Context("when a container already exists for the session", func() {
			var fakeContainer *wfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer = new(wfakes.FakeContainer)
				workerClient.FindContainerForIdentifierReturns(fakeContainer, true, nil)
			})

			It("does not error and returns a resource", func() {
				Expect(initErr).NotTo(HaveOccurred())
				Expect(initResource).NotTo(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})
		})
	})

	Describe("InitWithCache", func() {
		var (
			logger   *lagertest.TestLogger
			metadata Metadata = testMetadata{"a=1", "b=2"}

			initType        ResourceType
			cacheIdentifier *fakes.FakeCacheIdentifier

			initResource Resource
			initCache    Cache
			initErr      error
		)

		BeforeEach(func() {
			logger = lagertest.NewTestLogger("test")
			initType = "type1"
			cacheIdentifier = new(fakes.FakeCacheIdentifier)
		})

		JustBeforeEach(func() {
			initResource, initCache, initErr = tracker.InitWithCache(
				logger,
				metadata,
				session,
				initType,
				[]string{"resource", "tags"},
				cacheIdentifier,
			)
		})

		Context("when a container does not exist for the session", func() {
			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, nil)
			})

			Context("when a worker is found", func() {
				var satisfyingWorker *wfakes.FakeWorker

				BeforeEach(func() {
					satisfyingWorker = new(wfakes.FakeWorker)
					workerClient.SatisfyingReturns(satisfyingWorker, nil)

					satisfyingWorker.CreateContainerReturns(fakeContainer, nil)
				})

				Context("when the worker supports volume management", func() {
					var fakeBaggageclaimClient *bfakes.FakeClient

					BeforeEach(func() {
						fakeBaggageclaimClient = new(bfakes.FakeClient)
						satisfyingWorker.VolumeManagerReturns(fakeBaggageclaimClient, true)
					})

					Context("when the cache is already present", func() {
						var foundVolume *bfakes.FakeVolume

						BeforeEach(func() {
							foundVolume = new(bfakes.FakeVolume)
							foundVolume.HandleReturns("found-volume-handle")
							cacheIdentifier.FindOnReturns(foundVolume, true, nil)

							cacheIdentifier.ResourceVersionReturns(atc.Version{"some": "theversion"})
							cacheIdentifier.ResourceHashReturns("hash")
							satisfyingWorker.NameReturns("some-worker")
							foundVolume.ExpirationReturns(time.Hour, time.Now(), nil)
						})

						It("does not error and returns a resource", func() {
							Expect(initErr).NotTo(HaveOccurred())
							Expect(initResource).NotTo(BeNil())
						})

						It("chose the worker satisfying the resource type and tags", func() {
							Expect(workerClient.SatisfyingArgsForCall(0)).To(Equal(worker.WorkerSpec{
								ResourceType: "type1",
								Tags:         []string{"resource", "tags"},
							}))
						})

						It("located it on the correct worker", func() {
							Expect(cacheIdentifier.FindOnCallCount()).To(Equal(1))
							_, baggageclaimClient := cacheIdentifier.FindOnArgsForCall(0)
							Expect(baggageclaimClient).To(Equal(fakeBaggageclaimClient))
						})

						It("creates the container with the cache volume", func() {
							_, id, containerMetadata, spec := satisfyingWorker.CreateContainerArgsForCall(0)

							Expect(id).To(Equal(session.ID))
							Expect(containerMetadata).To(Equal(session.Metadata))
							resourceSpec := spec.(worker.ResourceTypeContainerSpec)

							Expect(resourceSpec.Type).To(Equal(string(initType)))
							Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
							Expect(resourceSpec.Ephemeral).To(Equal(true))
							Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
							Expect(resourceSpec.Cache).To(Equal(worker.VolumeMount{
								Volume:    foundVolume,
								MountPath: "/tmp/build/get",
							}))
						})

						It("saves the volume information to the database", func() {
							Expect(fakeDB.InsertVolumeCallCount()).To(Equal(1))
							Expect(fakeDB.InsertVolumeArgsForCall(0)).To(Equal(db.Volume{
								Handle:          "found-volume-handle",
								WorkerName:      "some-worker",
								TTL:             time.Hour,
								ResourceVersion: atc.Version{"some": "theversion"},
								ResourceHash:    "hash",
							}))
						})

						It("releases the volume, since the container keeps it alive", func() {
							Expect(foundVolume.ReleaseCallCount()).To(Equal(1))
						})

						Describe("the cache", func() {
							Describe("IsInitialized", func() {
								Context("when the volume has the initialized property set", func() {
									BeforeEach(func() {
										foundVolume.PropertiesReturns(baggageclaim.VolumeProperties{
											"initialized": "any-value",
										}, nil)
									})

									It("returns true", func() {
										Expect(initCache.IsInitialized()).To(BeTrue())
									})
								})

								Context("when the volume has no initialized property", func() {
									BeforeEach(func() {
										foundVolume.PropertiesReturns(baggageclaim.VolumeProperties{}, nil)
									})

									It("returns false", func() {
										initialized, err := initCache.IsInitialized()
										Expect(initialized).To(BeFalse())
										Expect(err).ToNot(HaveOccurred())
									})
								})

								Context("when getting the properties fails", func() {
									disaster := errors.New("nope")

									BeforeEach(func() {
										foundVolume.PropertiesReturns(nil, disaster)
									})

									It("returns the error", func() {
										_, err := initCache.IsInitialized()
										Expect(err).To(Equal(disaster))
									})
								})
							})

							Describe("Initialize", func() {
								It("sets the initialized property on the volume", func() {
									Expect(initCache.Initialize()).To(Succeed())

									Expect(foundVolume.SetPropertyCallCount()).To(Equal(1))
									name, value := foundVolume.SetPropertyArgsForCall(0)
									Expect(name).To(Equal("initialized"))
									Expect(value).To(Equal("yep"))
								})

								Context("when setting the property fails", func() {
									disaster := errors.New("nope")

									BeforeEach(func() {
										foundVolume.SetPropertyReturns(disaster)
									})

									It("returns the error", func() {
										err := initCache.Initialize()
										Expect(err).To(Equal(disaster))
									})
								})
							})
						})
					})

					Context("when an initialized volume for the cache is not present", func() {
						var createdVolume *bfakes.FakeVolume

						BeforeEach(func() {
							cacheIdentifier.FindOnReturns(nil, false, nil)

							createdVolume = new(bfakes.FakeVolume)
							createdVolume.HandleReturns("created-volume-handle")

							cacheIdentifier.CreateOnReturns(createdVolume, nil)
						})

						It("does not error and returns a resource", func() {
							Expect(initErr).NotTo(HaveOccurred())
							Expect(initResource).NotTo(BeNil())
						})

						It("chose the worker satisfying the resource type and tags", func() {
							Expect(workerClient.SatisfyingArgsForCall(0)).To(Equal(worker.WorkerSpec{
								ResourceType: "type1",
								Tags:         []string{"resource", "tags"},
							}))
						})

						It("created the volume on the right worker", func() {
							Expect(cacheIdentifier.CreateOnCallCount()).To(Equal(1))
							_, baggageclaimClient := cacheIdentifier.CreateOnArgsForCall(0)
							Expect(baggageclaimClient).To(Equal(fakeBaggageclaimClient))
						})

						It("creates the container with the created cache volume", func() {
							_, id, containerMetadata, spec := satisfyingWorker.CreateContainerArgsForCall(0)

							Expect(id).To(Equal(session.ID))
							Expect(containerMetadata).To(Equal(session.Metadata))
							resourceSpec := spec.(worker.ResourceTypeContainerSpec)

							Expect(resourceSpec.Type).To(Equal(string(initType)))
							Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
							Expect(resourceSpec.Ephemeral).To(Equal(true))
							Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
							Expect(resourceSpec.Cache).To(Equal(worker.VolumeMount{
								Volume:    createdVolume,
								MountPath: "/tmp/build/get",
							}))
						})

						It("releases the volume, since the container keeps it alive", func() {
							Expect(createdVolume.ReleaseCallCount()).To(Equal(1))
						})

						Describe("the cache", func() {
							Describe("IsInitialized", func() {
								Context("when the volume has the initialized property set", func() {
									BeforeEach(func() {
										createdVolume.PropertiesReturns(baggageclaim.VolumeProperties{
											"initialized": "any-value",
										}, nil)
									})

									It("returns true", func() {
										Expect(initCache.IsInitialized()).To(BeTrue())
									})
								})

								Context("when the volume has no initialized property", func() {
									BeforeEach(func() {
										createdVolume.PropertiesReturns(baggageclaim.VolumeProperties{}, nil)
									})

									It("returns false", func() {
										initialized, err := initCache.IsInitialized()
										Expect(initialized).To(BeFalse())
										Expect(err).ToNot(HaveOccurred())
									})
								})

								Context("when getting the properties fails", func() {
									disaster := errors.New("nope")

									BeforeEach(func() {
										createdVolume.PropertiesReturns(nil, disaster)
									})

									It("returns the error", func() {
										_, err := initCache.IsInitialized()
										Expect(err).To(Equal(disaster))
									})
								})
							})

							Describe("Initialize", func() {
								It("sets the initialized property on the volume", func() {
									Expect(initCache.Initialize()).To(Succeed())

									Expect(createdVolume.SetPropertyCallCount()).To(Equal(1))
									name, value := createdVolume.SetPropertyArgsForCall(0)
									Expect(name).To(Equal("initialized"))
									Expect(value).To(Equal("yep"))
								})

								Context("when setting the property fails", func() {
									disaster := errors.New("nope")

									BeforeEach(func() {
										createdVolume.SetPropertyReturns(disaster)
									})

									It("returns the error", func() {
										err := initCache.Initialize()
										Expect(err).To(Equal(disaster))
									})
								})
							})
						})
					})
				})

				Context("when the worker does not support volume management", func() {
					BeforeEach(func() {
						satisfyingWorker.VolumeManagerReturns(nil, false)
					})

					It("creates a container", func() {
						_, id, containerMetadata, spec := satisfyingWorker.CreateContainerArgsForCall(0)

						Expect(id).To(Equal(session.ID))
						Expect(containerMetadata).To(Equal(session.Metadata))
						resourceSpec := spec.(worker.ResourceTypeContainerSpec)

						Expect(resourceSpec.Type).To(Equal(string(initType)))
						Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
						Expect(resourceSpec.Ephemeral).To(Equal(true))
						Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
						Expect(resourceSpec.Cache).To(BeZero())
					})

					Context("when creating the container fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							satisfyingWorker.CreateContainerReturns(nil, disaster)
						})

						It("returns the error and no resource", func() {
							Expect(initErr).To(Equal(disaster))
							Expect(initResource).To(BeNil())
						})
					})
				})
			})

			Context("when no worker satisfies the spec", func() {
				disaster := errors.New("nope")

				BeforeEach(func() {
					workerClient.SatisfyingReturns(nil, disaster)
				})

				It("returns the error and no resource", func() {
					Expect(initErr).To(Equal(disaster))
					Expect(initResource).To(BeNil())
				})
			})
		})

		Context("when looking up the container fails for some reason", func() {
			disaster := errors.New("nope")

			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, disaster)
			})

			It("returns the error and no resource", func() {
				Expect(initErr).To(Equal(disaster))
				Expect(initResource).To(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.SatisfyingCallCount()).To(BeZero())
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})
		})

		Context("when a container already exists for the session", func() {
			var fakeContainer *wfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer = new(wfakes.FakeContainer)
				workerClient.FindContainerForIdentifierReturns(fakeContainer, true, nil)
			})

			It("does not error and returns a resource", func() {
				Expect(initErr).NotTo(HaveOccurred())
				Expect(initResource).NotTo(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.SatisfyingCallCount()).To(BeZero())
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})

			Context("when the container has a cache volume", func() {
				var cacheVolume *bfakes.FakeVolume

				BeforeEach(func() {
					cacheVolume = new(bfakes.FakeVolume)
					fakeContainer.VolumesReturns([]worker.Volume{cacheVolume})
				})

				Describe("the cache", func() {
					Describe("IsInitialized", func() {
						Context("when the volume has the initialized property set", func() {
							BeforeEach(func() {
								cacheVolume.PropertiesReturns(baggageclaim.VolumeProperties{
									"initialized": "any-value",
								}, nil)
							})

							It("returns true", func() {
								Expect(initCache.IsInitialized()).To(BeTrue())
							})
						})

						Context("when the volume has no initialized property", func() {
							BeforeEach(func() {
								cacheVolume.PropertiesReturns(baggageclaim.VolumeProperties{}, nil)
							})

							It("returns false", func() {
								initialized, err := initCache.IsInitialized()
								Expect(initialized).To(BeFalse())
								Expect(err).ToNot(HaveOccurred())
							})
						})

						Context("when getting the properties fails", func() {
							disaster := errors.New("nope")

							BeforeEach(func() {
								cacheVolume.PropertiesReturns(nil, disaster)
							})

							It("returns the error", func() {
								_, err := initCache.IsInitialized()
								Expect(err).To(Equal(disaster))
							})
						})
					})

					Describe("Initialize", func() {
						It("sets the initialized property on the volume", func() {
							Expect(initCache.Initialize()).To(Succeed())

							Expect(cacheVolume.SetPropertyCallCount()).To(Equal(1))
							name, value := cacheVolume.SetPropertyArgsForCall(0)
							Expect(name).To(Equal("initialized"))
							Expect(value).To(Equal("yep"))
						})

						Context("when setting the property fails", func() {
							disaster := errors.New("nope")

							BeforeEach(func() {
								cacheVolume.SetPropertyReturns(disaster)
							})

							It("returns the error", func() {
								err := initCache.Initialize()
								Expect(err).To(Equal(disaster))
							})
						})
					})
				})
			})

			Context("when the container has no volumes", func() {
				BeforeEach(func() {
					fakeContainer.VolumesReturns([]worker.Volume{})
				})

				Describe("the cache", func() {
					It("is not initialized", func() {
						initialized, err := initCache.IsInitialized()
						Expect(initialized).To(BeFalse())
						Expect(err).ToNot(HaveOccurred())
					})

					It("does a no-op initialize", func() {
						Expect(initCache.Initialize()).To(Succeed())
					})
				})
			})
		})
	})

	Describe("InitWithSources", func() {
		var (
			logger       *lagertest.TestLogger
			metadata     Metadata = testMetadata{"a=1", "b=2"}
			inputSources map[string]ArtifactSource

			inputSource1 *fakes.FakeArtifactSource
			inputSource2 *fakes.FakeArtifactSource
			inputSource3 *fakes.FakeArtifactSource

			initType ResourceType

			initResource   Resource
			missingSources []string
			initErr        error
		)

		BeforeEach(func() {
			logger = lagertest.NewTestLogger("test")
			initType = "type1"

			inputSource1 = new(fakes.FakeArtifactSource)
			inputSource2 = new(fakes.FakeArtifactSource)
			inputSource3 = new(fakes.FakeArtifactSource)

			inputSources = map[string]ArtifactSource{
				"source-1-name": inputSource1,
				"source-2-name": inputSource2,
				"source-3-name": inputSource3,
			}
		})

		JustBeforeEach(func() {
			initResource, missingSources, initErr = tracker.InitWithSources(
				logger,
				metadata,
				session,
				initType,
				[]string{"resource", "tags"},
				inputSources,
			)
		})

		Context("when a container does not exist for the session", func() {
			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, nil)
			})

			Context("when a worker is found", func() {
				var satisfyingWorker *wfakes.FakeWorker

				BeforeEach(func() {
					satisfyingWorker = new(wfakes.FakeWorker)
					workerClient.AllSatisfyingReturns([]worker.Worker{satisfyingWorker}, nil)

					satisfyingWorker.CreateContainerReturns(fakeContainer, nil)
				})

				Context("when some volumes are found on the worker", func() {
					var (
						inputVolume1 *bfakes.FakeVolume
						inputVolume3 *bfakes.FakeVolume
					)

					BeforeEach(func() {
						inputVolume1 = new(bfakes.FakeVolume)
						inputVolume3 = new(bfakes.FakeVolume)

						inputSource1.VolumeOnReturns(inputVolume1, true, nil)
						inputSource2.VolumeOnReturns(nil, false, nil)
						inputSource3.VolumeOnReturns(inputVolume3, true, nil)
					})

					It("does not error and returns a resource", func() {
						Expect(initErr).NotTo(HaveOccurred())
						Expect(initResource).NotTo(BeNil())
					})

					It("chose the worker satisfying the resource type and tags", func() {
						Expect(workerClient.AllSatisfyingCallCount()).To(Equal(1))
						Expect(workerClient.AllSatisfyingArgsForCall(0)).To(Equal(worker.WorkerSpec{
							ResourceType: "type1",
							Tags:         []string{"resource", "tags"},
						}))
					})

					It("looked for the sources on the correct worker", func() {
						Expect(inputSource1.VolumeOnCallCount()).To(Equal(1))
						actualWorker := inputSource1.VolumeOnArgsForCall(0)
						Expect(actualWorker).To(Equal(satisfyingWorker))

						Expect(inputSource2.VolumeOnCallCount()).To(Equal(1))
						actualWorker = inputSource2.VolumeOnArgsForCall(0)
						Expect(actualWorker).To(Equal(satisfyingWorker))

						Expect(inputSource3.VolumeOnCallCount()).To(Equal(1))
						actualWorker = inputSource3.VolumeOnArgsForCall(0)
						Expect(actualWorker).To(Equal(satisfyingWorker))
					})

					It("creates the container with the cache volume", func() {
						Expect(satisfyingWorker.CreateContainerCallCount()).To(Equal(1))
						_, id, containerMetadata, spec := satisfyingWorker.CreateContainerArgsForCall(0)

						Expect(id).To(Equal(session.ID))
						Expect(containerMetadata).To(Equal(session.Metadata))
						resourceSpec := spec.(worker.ResourceTypeContainerSpec)

						Expect(resourceSpec.Type).To(Equal(string(initType)))
						Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
						Expect(resourceSpec.Ephemeral).To(BeTrue())
						Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
						Expect(resourceSpec.Mounts).To(ConsistOf([]worker.VolumeMount{
							{
								Volume:    inputVolume1,
								MountPath: "/tmp/build/put/source-1-name",
							},
							{
								Volume:    inputVolume3,
								MountPath: "/tmp/build/put/source-3-name",
							},
						}))
					})

					It("releases the volume, since the container keeps it alive", func() {
						Expect(inputVolume1.ReleaseCallCount()).To(Equal(1))
						Expect(inputVolume3.ReleaseCallCount()).To(Equal(1))
					})

					It("returns the artifact sources that it could not find volumes for", func() {
						Expect(missingSources).To(ConsistOf("source-2-name"))
					})

					Context("when creating the container fails", func() {
						disaster := errors.New("oh no!")

						BeforeEach(func() {
							satisfyingWorker.CreateContainerReturns(nil, disaster)
						})

						It("returns the error and no resource", func() {
							Expect(initErr).To(Equal(disaster))
							Expect(missingSources).To(BeNil())
							Expect(initResource).To(BeNil())
						})
					})
				})

				Context("when there are no volumes on the container (e.g. doesn't support volumes)", func() {
					BeforeEach(func() {
						inputSource1.VolumeOnReturns(nil, false, nil)
						inputSource2.VolumeOnReturns(nil, false, nil)
						inputSource3.VolumeOnReturns(nil, false, nil)
					})

					It("creates a container with no volumes", func() {
						Expect(satisfyingWorker.CreateContainerCallCount()).To(Equal(1))
						_, id, containerMetadata, spec := satisfyingWorker.CreateContainerArgsForCall(0)

						Expect(id).To(Equal(session.ID))
						Expect(containerMetadata).To(Equal(session.Metadata))
						resourceSpec := spec.(worker.ResourceTypeContainerSpec)

						Expect(resourceSpec.Type).To(Equal(string(initType)))
						Expect(resourceSpec.Env).To(Equal([]string{"a=1", "b=2"}))
						Expect(resourceSpec.Ephemeral).To(Equal(true))
						Expect(resourceSpec.Tags).To(ConsistOf("resource", "tags"))
						Expect(resourceSpec.Cache).To(BeZero())
					})

					It("returns them all as missing sources", func() {
						Expect(missingSources).To(ConsistOf("source-1-name", "source-2-name", "source-3-name"))
					})
				})

				Context("when looking up one of the volumes fails", func() {
					disaster := errors.New("nope")

					BeforeEach(func() {
						inputSource1.VolumeOnReturns(nil, false, nil)
						inputSource2.VolumeOnReturns(nil, false, disaster)
						inputSource3.VolumeOnReturns(nil, false, nil)
					})

					It("returns the error and no resource", func() {
						Expect(initErr).To(Equal(disaster))
						Expect(missingSources).To(BeNil())
						Expect(initResource).To(BeNil())
					})
				})
			})

			Context("when multiple workers satisfy the spec", func() {
				var (
					satisfyingWorker1 *wfakes.FakeWorker
					satisfyingWorker2 *wfakes.FakeWorker
					satisfyingWorker3 *wfakes.FakeWorker
				)

				BeforeEach(func() {
					satisfyingWorker1 = new(wfakes.FakeWorker)
					satisfyingWorker2 = new(wfakes.FakeWorker)
					satisfyingWorker3 = new(wfakes.FakeWorker)

					workerClient.AllSatisfyingReturns([]worker.Worker{
						satisfyingWorker1,
						satisfyingWorker2,
						satisfyingWorker3,
					}, nil)

					satisfyingWorker1.CreateContainerReturns(fakeContainer, nil)
					satisfyingWorker2.CreateContainerReturns(fakeContainer, nil)
					satisfyingWorker3.CreateContainerReturns(fakeContainer, nil)
				})

				Context("and some workers have more matching input volumes than others", func() {
					var inputVolume *bfakes.FakeVolume
					var inputVolume2 *bfakes.FakeVolume
					var inputVolume3 *bfakes.FakeVolume
					var otherInputVolume *bfakes.FakeVolume

					BeforeEach(func() {
						inputVolume = new(bfakes.FakeVolume)
						inputVolume.HandleReturns("input-volume-1")

						inputVolume2 = new(bfakes.FakeVolume)
						inputVolume2.HandleReturns("input-volume-2")

						inputVolume3 = new(bfakes.FakeVolume)
						inputVolume3.HandleReturns("input-volume-3")

						otherInputVolume = new(bfakes.FakeVolume)
						otherInputVolume.HandleReturns("other-input-volume")

						inputSource1.VolumeOnStub = func(w worker.Worker) (baggageclaim.Volume, bool, error) {
							if w == satisfyingWorker1 {
								return inputVolume, true, nil
							} else if w == satisfyingWorker2 {
								return inputVolume2, true, nil
							} else if w == satisfyingWorker3 {
								return inputVolume3, true, nil
							} else {
								return nil, false, fmt.Errorf("unexpected worker: %#v\n", w)
							}
						}
						inputSource2.VolumeOnStub = func(w worker.Worker) (baggageclaim.Volume, bool, error) {
							if w == satisfyingWorker1 {
								return nil, false, nil
							} else if w == satisfyingWorker2 {
								return otherInputVolume, true, nil
							} else if w == satisfyingWorker3 {
								return nil, false, nil
							} else {
								return nil, false, fmt.Errorf("unexpected worker: %#v\n", w)
							}
						}
						inputSource3.VolumeOnReturns(nil, false, nil)

						satisfyingWorker1.CreateContainerReturns(nil, errors.New("fall out of method here"))
						satisfyingWorker2.CreateContainerReturns(nil, errors.New("fall out of method here"))
						satisfyingWorker3.CreateContainerReturns(nil, errors.New("fall out of method here"))
					})

					It("picks the worker that has the most", func() {
						Expect(satisfyingWorker1.CreateContainerCallCount()).To(Equal(0))
						Expect(satisfyingWorker2.CreateContainerCallCount()).To(Equal(1))
						Expect(satisfyingWorker3.CreateContainerCallCount()).To(Equal(0))
					})

					It("releases the volumes on the unused workers", func() {
						Expect(inputVolume.ReleaseCallCount()).To(Equal(1))
						Expect(inputVolume3.ReleaseCallCount()).To(Equal(1))

						// We don't expect these to be released because we are
						// causing an error in the create container step, which
						// happens before they are released.
						Expect(inputVolume2.ReleaseCallCount()).To(Equal(0))
						Expect(otherInputVolume.ReleaseCallCount()).To(Equal(0))
					})
				})
			})

			Context("when no worker satisfies the spec", func() {
				disaster := errors.New("nope")

				BeforeEach(func() {
					workerClient.AllSatisfyingReturns(nil, disaster)
				})

				It("returns the error and no resource", func() {
					Expect(initErr).To(Equal(disaster))
					Expect(missingSources).To(BeNil())
					Expect(initResource).To(BeNil())
				})
			})
		})

		Context("when looking up the container fails for some reason", func() {
			disaster := errors.New("nope")

			BeforeEach(func() {
				workerClient.FindContainerForIdentifierReturns(nil, false, disaster)
			})

			It("returns the error and no resource", func() {
				Expect(initErr).To(Equal(disaster))
				Expect(missingSources).To(BeNil())
				Expect(initResource).To(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.SatisfyingCallCount()).To(BeZero())
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})
		})

		Context("when a container already exists for the session", func() {
			var fakeContainer *wfakes.FakeContainer

			BeforeEach(func() {
				fakeContainer = new(wfakes.FakeContainer)
				workerClient.FindContainerForIdentifierReturns(fakeContainer, true, nil)
			})

			It("does not error and returns a resource", func() {
				Expect(initErr).NotTo(HaveOccurred())
				Expect(initResource).NotTo(BeNil())
			})

			It("does not create a container", func() {
				Expect(workerClient.SatisfyingCallCount()).To(BeZero())
				Expect(workerClient.CreateContainerCallCount()).To(BeZero())
			})

			It("returns them all as missing sources", func() {
				Expect(missingSources).To(ConsistOf("source-1-name", "source-2-name", "source-3-name"))
			})
		})
	})
})
