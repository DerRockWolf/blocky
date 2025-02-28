package resolver

import (
	"time"

	"github.com/0xERR0R/blocky/config"
	. "github.com/0xERR0R/blocky/helpertest"
	"github.com/0xERR0R/blocky/log"
	. "github.com/0xERR0R/blocky/model"
	"github.com/0xERR0R/blocky/util"
	"github.com/miekg/dns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StrictResolver", Label("strictResolver"), func() {
	const (
		verifyUpstreams   = true
		noVerifyUpstreams = false
	)

	var (
		sut        *StrictResolver
		sutMapping config.UpstreamGroups
		sutVerify  bool

		err error

		bootstrap *Bootstrap
	)

	Describe("Type", func() {
		It("follows conventions", func() {
			expectValidResolverType(sut)
		})
	})

	BeforeEach(func() {
		sutMapping = config.UpstreamGroups{
			upstreamDefaultCfgName: {
				config.Upstream{
					Host: "wrong",
				},
				config.Upstream{
					Host: "127.0.0.2",
				},
			},
		}

		sutVerify = noVerifyUpstreams

		bootstrap = systemResolverBootstrap
	})

	JustBeforeEach(func() {
		sutConfig := config.UpstreamsConfig{Groups: sutMapping}

		sut, err = NewStrictResolver(sutConfig, bootstrap, sutVerify)
	})

	config.GetConfig().Upstreams.Timeout = config.Duration(1000 * time.Millisecond)

	Describe("IsEnabled", func() {
		It("is true", func() {
			Expect(sut.IsEnabled()).Should(BeTrue())
		})
	})

	Describe("LogConfig", func() {
		It("should log something", func() {
			logger, hook := log.NewMockEntry()

			sut.LogConfig(logger)

			Expect(hook.Calls).ShouldNot(BeEmpty())
		})
	})

	Describe("Type", func() {
		It("should be correct", func() {
			Expect(sut.Type()).ShouldNot(BeEmpty())
			Expect(sut.Type()).Should(Equal(strictResolverType))
		})
	})

	Describe("Name", func() {
		It("should contain correct resolver", func() {
			Expect(sut.Name()).ShouldNot(BeEmpty())
			Expect(sut.Name()).Should(ContainSubstring(strictResolverType))
		})
	})

	When("some default upstream resolvers cannot be reached", func() {
		It("should start normally", func() {
			mockUpstream := NewMockUDPUpstreamServer().WithAnswerFn(func(request *dns.Msg) (response *dns.Msg) {
				response, _ = util.NewMsgWithAnswer(request.Question[0].Name, 123, A, "123.124.122.122")

				return
			})
			defer mockUpstream.Close()

			upstream := config.UpstreamGroups{
				upstreamDefaultCfgName: {
					config.Upstream{
						Host: "wrong",
					},
					mockUpstream.Start(),
				},
			}

			_, err := NewStrictResolver(config.UpstreamsConfig{
				Groups: upstream,
			}, systemResolverBootstrap, verifyUpstreams)
			Expect(err).Should(Not(HaveOccurred()))
		})
	})

	When("no upstream resolvers can be reached", func() {
		BeforeEach(func() {
			sutMapping = config.UpstreamGroups{
				upstreamDefaultCfgName: {
					config.Upstream{
						Host: "wrong",
					},
					config.Upstream{
						Host: "127.0.0.2",
					},
				},
			}
		})

		When("strict checking is enabled", func() {
			BeforeEach(func() {
				sutVerify = verifyUpstreams
			})
			It("should fail to start", func() {
				Expect(err).Should(HaveOccurred())
			})
		})

		When("strict checking is disabled", func() {
			BeforeEach(func() {
				sutVerify = noVerifyUpstreams
			})
			It("should start", func() {
				Expect(err).Should(Not(HaveOccurred()))
			})
		})
	})

	Describe("Resolving request in strict order", func() {
		When("2 Upstream resolvers are defined", func() {
			When("Both are responding", func() {
				When("they respond in time", func() {
					BeforeEach(func() {
						testUpstream1 := NewMockUDPUpstreamServer().WithAnswerRR("example.com 123 IN A 123.124.122.122")
						DeferCleanup(testUpstream1.Close)

						testUpstream2 := NewMockUDPUpstreamServer().WithAnswerRR("example.com 123 IN A 123.124.122.123")
						DeferCleanup(testUpstream2.Close)

						sutMapping = config.UpstreamGroups{
							upstreamDefaultCfgName: {testUpstream1.Start(), testUpstream2.Start()},
						}
					})
					It("Should use result from first one", func() {
						request := newRequest("example.com.", A)
						Expect(sut.Resolve(request)).
							Should(
								SatisfyAll(
									BeDNSRecord("example.com.", A, "123.124.122.122"),
									HaveTTL(BeNumerically("==", 123)),
									HaveResponseType(ResponseTypeRESOLVED),
									HaveReturnCode(dns.RcodeSuccess),
								))
					})
				})
				When("first upstream exceeds upstreamTimeout", func() {
					BeforeEach(func() {
						testUpstream1 := NewMockUDPUpstreamServer().WithAnswerFn(func(request *dns.Msg) (response *dns.Msg) {
							response, err := util.NewMsgWithAnswer("example.com", 123, A, "123.124.122.1")
							time.Sleep(time.Duration(config.GetConfig().Upstreams.Timeout) + 2*time.Second)

							Expect(err).To(Succeed())

							return response
						})
						DeferCleanup(testUpstream1.Close)

						testUpstream2 := NewMockUDPUpstreamServer().WithAnswerRR("example.com 123 IN A 123.124.122.2")
						DeferCleanup(testUpstream2.Close)

						sutMapping = config.UpstreamGroups{
							upstreamDefaultCfgName: {testUpstream1.Start(), testUpstream2.Start()},
						}
					})
					It("should return response from next upstream", func() {
						request := newRequest("example.com", A)
						Expect(sut.Resolve(request)).Should(
							SatisfyAll(
								BeDNSRecord("example.com.", A, "123.124.122.2"),
								HaveTTL(BeNumerically("==", 123)),
								HaveResponseType(ResponseTypeRESOLVED),
								HaveReturnCode(dns.RcodeSuccess),
							))
					})
				})
				When("all upstreams exceed upsteamTimeout", func() {
					BeforeEach(func() {
						testUpstream1 := NewMockUDPUpstreamServer().WithAnswerFn(func(request *dns.Msg) (response *dns.Msg) {
							response, err := util.NewMsgWithAnswer("example.com", 123, A, "123.124.122.1")
							time.Sleep(config.GetConfig().Upstreams.Timeout.ToDuration() + 2*time.Second)

							Expect(err).To(Succeed())

							return response
						})
						DeferCleanup(testUpstream1.Close)

						testUpstream2 := NewMockUDPUpstreamServer().WithAnswerFn(func(request *dns.Msg) (response *dns.Msg) {
							response, err := util.NewMsgWithAnswer("example.com", 123, A, "123.124.122.2")
							time.Sleep(config.GetConfig().Upstreams.Timeout.ToDuration() + 2*time.Second)

							Expect(err).To(Succeed())

							return response
						})
						DeferCleanup(testUpstream2.Close)

						sutMapping = config.UpstreamGroups{
							upstreamDefaultCfgName: {testUpstream1.Start(), testUpstream2.Start()},
						}
					})
					It("should return error", func() {
						request := newRequest("example.com", A)
						_, err := sut.Resolve(request)
						Expect(err).To(HaveOccurred())
					})
				})
			})
			When("Only second is working", func() {
				BeforeEach(func() {
					testUpstream1 := config.Upstream{Host: "wrong"}

					testUpstream2 := NewMockUDPUpstreamServer().WithAnswerRR("example.com 123 IN A 123.124.122.123")
					DeferCleanup(testUpstream2.Close)

					sutMapping = config.UpstreamGroups{
						upstreamDefaultCfgName: {testUpstream1, testUpstream2.Start()},
					}
				})
				It("Should use result from second one", func() {
					request := newRequest("example.com.", A)
					Expect(sut.Resolve(request)).
						Should(
							SatisfyAll(
								BeDNSRecord("example.com.", A, "123.124.122.123"),
								HaveTTL(BeNumerically("==", 123)),
								HaveResponseType(ResponseTypeRESOLVED),
								HaveReturnCode(dns.RcodeSuccess),
							))
				})
			})
			When("None are working", func() {
				BeforeEach(func() {
					testUpstream1 := config.Upstream{Host: "wrong"}
					testUpstream2 := config.Upstream{Host: "wrong"}

					sutMapping = config.UpstreamGroups{
						upstreamDefaultCfgName: {testUpstream1, testUpstream2},
					}
					Expect(err).Should(Succeed())
				})
				It("Should return error", func() {
					request := newRequest("example.com.", A)
					_, err := sut.Resolve(request)
					Expect(err).Should(HaveOccurred())
				})
			})
		})
		When("only 1 upstream resolvers is defined", func() {
			BeforeEach(func() {
				mockUpstream := NewMockUDPUpstreamServer().WithAnswerRR("example.com 123 IN A 123.124.122.122")
				DeferCleanup(mockUpstream.Close)

				sutMapping = config.UpstreamGroups{
					upstreamDefaultCfgName: {
						mockUpstream.Start(),
					},
				}
			})
			It("Should use result from defined resolver", func() {
				request := newRequest("example.com.", A)

				Expect(sut.Resolve(request)).
					Should(
						SatisfyAll(
							BeDNSRecord("example.com.", A, "123.124.122.122"),
							HaveTTL(BeNumerically("==", 123)),
							HaveResponseType(ResponseTypeRESOLVED),
							HaveReturnCode(dns.RcodeSuccess),
						))
			})
		})
	})
})
