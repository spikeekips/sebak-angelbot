package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	logging "github.com/inconshreveable/log15"
	"github.com/spf13/cobra"
	"github.com/stellar/go/keypair"
	"github.com/ulule/limiter"
	"golang.org/x/net/http2"

	cmdcommon "boscoin.io/sebak/cmd/sebak/common"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/network"
	"boscoin.io/sebak/lib/node"
)

const (
	defaultSEBAKEndpoint string      = "https://localhost:12345"
	defaultBind          string      = "http://localhost:23456"
	defaultHost          string      = "0.0.0.0"
	defaultLogLevel      logging.Lvl = logging.LvlInfo
)

var (
	flagSecretSeed          string              = common.GetENVValue("SEBAK_SECRET_SEED", "")
	flagNetworkID           string              = common.GetENVValue("SEBAK_NETWORK_ID", "")
	flagLogLevel            string              = common.GetENVValue("SEBAK_LOG_LEVEL", defaultLogLevel.String())
	flagLogOutput           string              = common.GetENVValue("SEBAK_LOG_OUTPUT", "")
	flagVerbose             bool                = common.GetENVValue("SEBAK_VERBOSE", "0") == "1"
	flagBind                string              = common.GetENVValue("SEBAK_BIND", defaultBind)
	flagSEBAKEndpointString string              = common.GetENVValue("SEBAK_SEBAK_ENDPOINT", defaultSEBAKEndpoint)
	flagTLSCertFile         string              = common.GetENVValue("SEBAK_TLS_CERT", "sebak.crt")
	flagTLSKeyFile          string              = common.GetENVValue("SEBAK_TLS_KEY", "sebak.key")
	flagSources             string              = common.GetENVValue("SEBAK_SOURCES", "")
	flagRateLimit           cmdcommon.ListFlags // "SEBAK_RATE_LIMIT"
	flagMaxBalance          string              = common.GetENVValue("SEBAK_MAX_BALANCE", defaultMaxBalance)
)

var (
	runCmd *cobra.Command

	kp               *keypair.Full
	sebakEndpoint    *common.Endpoint
	bindURL          *url.URL
	logLevel         logging.Lvl
	log              logging.Logger
	sources          map[string]*Account = map[string]*Account{}
	verbose          bool
	rateLimitRule    common.RateLimitRule
	defaultRateLimit limiter.Rate = limiter.Rate{
		Period: 1 * time.Minute,
		Limit:  100,
	}
	defaultMaxBalance string = strconv.FormatUint(uint64(common.BaseReserve*100000), 10)
	maxBalance        common.Amount
)

func init() {
	runCmd = &cobra.Command{
		Use:   "run",
		Short: "run sebak-angelbot",
		Args:  cobra.ExactArgs(0),
		Run: func(c *cobra.Command, args []string) {
			parseFlagsNode()

			run()
			return
		},
	}

	runCmd.Flags().StringVar(&flagSecretSeed, "secret-seed", flagSecretSeed, "secret seed of master account")
	runCmd.Flags().StringVar(&flagNetworkID, "network-id", flagNetworkID, "network id")
	runCmd.Flags().StringVar(&flagLogLevel, "log-level", flagLogLevel, "log level, {crit, error, warn, info, debug}")
	runCmd.Flags().StringVar(&flagLogOutput, "log-output", flagLogOutput, "set log output file")
	runCmd.Flags().BoolVar(&flagVerbose, "verbose", flagVerbose, "verbose")
	runCmd.Flags().StringVar(&flagSEBAKEndpointString, "sebak-endpoint", flagSEBAKEndpointString, "sebak endpoint uri")
	runCmd.Flags().StringVar(&flagBind, "bind", flagBind, "bind address")
	runCmd.Flags().StringVar(&flagTLSCertFile, "tls-cert", flagTLSCertFile, "tls certificate file")
	runCmd.Flags().StringVar(&flagTLSKeyFile, "tls-key", flagTLSKeyFile, "tls key file")
	runCmd.Flags().StringVar(&flagSources, "sources", flagSources, "source account list file")
	runCmd.Flags().StringVar(&flagMaxBalance, "max-balance", flagMaxBalance, "maximum balance for new account")
	runCmd.Flags().Var(
		&flagRateLimit,
		"rate-limit",
		"rate limit: [<ip>=]<limit>-<period>, ex) '10-S' '3.3.3.3=1000-M'",
	)

	rootCmd.AddCommand(runCmd)
}

func parseFlagsNode() {
	var err error

	if len(flagNetworkID) < 1 {
		cmdcommon.PrintFlagsError(runCmd, "--network-id", errors.New("must be given"))
	}
	if len(flagSecretSeed) < 1 {
		cmdcommon.PrintFlagsError(runCmd, "--secret-seed", errors.New("must be given"))
	}

	var parsedKP keypair.KP
	parsedKP, err = keypair.Parse(flagSecretSeed)
	if err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--secret-seed", err)
	} else {
		kp = parsedKP.(*keypair.Full)
	}

	if bindURL, err = url.Parse(flagBind); err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--bind", err)
	}

	if p, err := common.ParseEndpoint(flagSEBAKEndpointString); err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--endpoint", err)
	} else {
		sebakEndpoint = p
		flagSEBAKEndpointString = sebakEndpoint.String()
	}

	if bindURL.Scheme == "https" {
		if _, err = os.Stat(flagTLSCertFile); os.IsNotExist(err) {
			cmdcommon.PrintFlagsError(runCmd, "--tls-cert", err)
		}
		if _, err = os.Stat(flagTLSKeyFile); os.IsNotExist(err) {
			cmdcommon.PrintFlagsError(runCmd, "--tls-key", err)
		}
	}

	if maxBalance, err = common.AmountFromString(flagMaxBalance); err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--max-balance", err)
	}

	if len(flagSources) < 1 {
		cmdcommon.PrintFlagsError(runCmd, "--sources", errors.New("must be given"))
	}

	sourcesF, err := os.Open(flagSources)
	if err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--sources", err)
	}

	{
		scanner := bufio.NewScanner(sourcesF)
		for scanner.Scan() {
			s := scanner.Text()
			sp := strings.Fields(s)
			if len(sp) < 1 {
				cmdcommon.PrintFlagsError(runCmd, "--sources", fmt.Errorf("invalid line found: '%s'", s))
			}
			kp, err := keypair.Parse(sp[0])
			if err != nil {
				cmdcommon.PrintFlagsError(runCmd, "--sources", err)
			}
			kpFull, ok := kp.(*keypair.Full)
			if !ok {
				cmdcommon.PrintFlagsError(runCmd, "--sources", fmt.Errorf("invalid secret seed found: '%s'", s))
			}

			sources[kpFull.Address()] = &Account{KP: kpFull}
		}
		if err := scanner.Err(); err != nil {
			cmdcommon.PrintFlagsError(runCmd, "--sources", err)
		}
	}
	if len(sources) < 1 {
		cmdcommon.PrintFlagsError(runCmd, "--sources", errors.New("sources are empty"))
	}

	rateLimitRule, err = parseFlagRateLimit(flagRateLimit, defaultRateLimit)
	if err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--rate-limit", err)
	}

	queries := sebakEndpoint.Query()
	queries.Add("TLSCertFile", flagTLSCertFile)
	queries.Add("TLSKeyFile", flagTLSKeyFile)
	queries.Add("IdleTimeout", "3s")
	queries.Add("NodeName", node.MakeAlias(kp.Address()))
	sebakEndpoint.RawQuery = queries.Encode()

	if logLevel, err = logging.LvlFromString(flagLogLevel); err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--log-level", err)
	}

	logHandler := logging.StdoutHandler

	if len(flagLogOutput) < 1 {
		flagLogOutput = "<stdout>"
	} else {
		if logHandler, err = logging.FileHandler(flagLogOutput, logging.JsonFormat()); err != nil {
			cmdcommon.PrintFlagsError(runCmd, "--log-output", err)
		}
	}

	log = logging.New("module", "main")
	log.SetHandler(logging.LvlFilterHandler(logLevel, logHandler))
	network.SetLogging(logLevel, logHandler)

	log.Info("Starting sebak angelbot")

	// print flags
	parsedFlags := []interface{}{}
	parsedFlags = append(parsedFlags, "\n\tnetwork-id", flagNetworkID)
	parsedFlags = append(parsedFlags, "\n\tsebak endpoint", flagSEBAKEndpointString)
	parsedFlags = append(parsedFlags, "\n\tbind", flagBind)
	parsedFlags = append(parsedFlags, "\n\ttls-cert", flagTLSCertFile)
	parsedFlags = append(parsedFlags, "\n\ttls-key", flagTLSKeyFile)
	parsedFlags = append(parsedFlags, "\n\tlog-level", flagLogLevel)
	parsedFlags = append(parsedFlags, "\n\tlog-output", flagLogOutput)
	parsedFlags = append(parsedFlags, "\n\tsources", len(sources))
	parsedFlags = append(parsedFlags, "\n\tmax-balance", maxBalance)

	log.Debug("parsed flags:", parsedFlags...)

	// check node status
	http2Client, _ := common.NewHTTP2Client(
		3*time.Second,
		3*time.Second,
		false,
	)
	client := network.NewHTTP2NetworkClient(sebakEndpoint, http2Client)
	if _, err := client.GetNodeInfo(); err != nil {
		cmdcommon.PrintFlagsError(runCmd, "--sebak-endpoint", err)
	}

	if flagVerbose {
		http2.VerboseLogs = true
		verbose = true
	}
}

func parseFlagRateLimit(l cmdcommon.ListFlags, defaultRate limiter.Rate) (rule common.RateLimitRule, err error) {
	if len(l) < 1 {
		rule = common.NewRateLimitRule(defaultRate)
		return
	}

	var givenRate limiter.Rate

	byIPAddress := map[string]limiter.Rate{}
	for _, s := range l {
		sl := strings.SplitN(s, "=", 2)

		var ip, r string
		if len(sl) < 2 {
			r = s
		} else {
			ip = sl[0]
			r = sl[1]
		}

		if len(ip) > 0 {
			if net.ParseIP(ip) == nil {
				err = fmt.Errorf("invalid ip address")
				return
			}
		}

		var rate limiter.Rate
		if rate, err = limiter.NewRateFromFormatted(r); err != nil {
			return
		}

		if len(ip) > 0 {
			byIPAddress[ip] = rate
		} else {
			givenRate = rate
		}
	}

	if givenRate.Period < 1 && givenRate.Limit < 1 {
		givenRate = defaultRate
	}

	rule = common.NewRateLimitRule(givenRate)
	rule.ByIPAddress = byIPAddress

	return
}

func run() {
	am := NewAccountManager([]byte(flagNetworkID), kp, sebakEndpoint, sources)
	am.Start()

	server := &http.Server{Addr: bindURL.Host}
	server.SetKeepAlivesEnabled(false)

	http2.ConfigureServer(server, &http2.Server{})

	handler := &Handler{
		am:            am,
		kp:            kp,
		sebakEndpoint: sebakEndpoint,
		networkID:     []byte(flagNetworkID),
	}
	router := mux.NewRouter()

	router.Use(network.RateLimitMiddleware(log, rateLimitRule))

	router.HandleFunc("/account/{address}", handler.accountHandler).Methods("POST", "GET", "OPTIONS")
	router.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	server.Handler = handlers.CombinedLoggingHandler(os.Stdout, router)

	var err error
	if bindURL.Scheme == "https" {
		err = server.ListenAndServeTLS(flagTLSCertFile, flagTLSKeyFile)
	} else {
		err = server.ListenAndServe()
	}
	log.Crit("something wrong", "error", err)

	return
}
