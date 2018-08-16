package cmd

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	logging "github.com/inconshreveable/log15"
	"github.com/spf13/cobra"
	"github.com/stellar/go/keypair"
	"golang.org/x/net/http2"

	"boscoin.io/sebak/cmd/sebak/common"
	"boscoin.io/sebak/lib"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/network"
)

const (
	defaultSEBAKEndpoint string      = "https://localhost:12345"
	defaultBind          string      = "localhost:23456"
	defaultHost          string      = "0.0.0.0"
	defaultLogLevel      logging.Lvl = logging.LvlInfo
)

var (
	flagSecretSeed          string = sebakcommon.GetENVValue("SEBAK_SECRET_SEED", "")
	flagNetworkID           string = sebakcommon.GetENVValue("SEBAK_NETWORK_ID", "")
	flagLogLevel            string = sebakcommon.GetENVValue("SEBAK_LOG_LEVEL", defaultLogLevel.String())
	flagLogOutput           string = sebakcommon.GetENVValue("SEBAK_LOG_OUTPUT", "")
	flagVerbose             bool   = sebakcommon.GetENVValue("SEBAK_VERBOSE", "0") == "1"
	flagBind                string = sebakcommon.GetENVValue("SEBAK_BIND", defaultBind)
	flagSEBAKEndpointString string = sebakcommon.GetENVValue("SEBAK_SEBAK_ENDPOINT", defaultSEBAKEndpoint)
	flagTLSCertFile         string = sebakcommon.GetENVValue("SEBAK_TLS_CERT", "sebak.crt")
	flagTLSKeyFile          string = sebakcommon.GetENVValue("SEBAK_TLS_KEY", "sebak.key")
)

var (
	runCmd *cobra.Command

	kp            *keypair.Full
	sebakEndpoint *sebakcommon.Endpoint
	logLevel      logging.Lvl
	log           logging.Logger
	verbose       bool
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

	rootCmd.AddCommand(runCmd)
}

func parseFlagsNode() {
	var err error

	if len(flagNetworkID) < 1 {
		common.PrintFlagsError(runCmd, "--network-id", errors.New("must be given"))
	}
	if len(flagSecretSeed) < 1 {
		common.PrintFlagsError(runCmd, "--secret-seed", errors.New("must be given"))
	}

	var parsedKP keypair.KP
	parsedKP, err = keypair.Parse(flagSecretSeed)
	if err != nil {
		common.PrintFlagsError(runCmd, "--secret-seed", err)
	} else {
		kp = parsedKP.(*keypair.Full)
	}

	if p, err := sebakcommon.ParseNodeEndpoint(flagSEBAKEndpointString); err != nil {
		common.PrintFlagsError(runCmd, "--endpoint", err)
	} else {
		sebakEndpoint = p
		flagSEBAKEndpointString = sebakEndpoint.String()
	}

	if _, err = os.Stat(flagTLSCertFile); os.IsNotExist(err) {
		common.PrintFlagsError(runCmd, "--tls-cert", err)
	}
	if _, err = os.Stat(flagTLSKeyFile); os.IsNotExist(err) {
		common.PrintFlagsError(runCmd, "--tls-key", err)
	}

	queries := sebakEndpoint.Query()
	queries.Add("TLSCertFile", flagTLSCertFile)
	queries.Add("TLSKeyFile", flagTLSKeyFile)
	queries.Add("IdleTimeout", "3s")
	queries.Add("NodeName", sebakcommon.MakeAlias(kp.Address()))
	sebakEndpoint.RawQuery = queries.Encode()

	if logLevel, err = logging.LvlFromString(flagLogLevel); err != nil {
		common.PrintFlagsError(runCmd, "--log-level", err)
	}

	logHandler := logging.StdoutHandler

	if len(flagLogOutput) < 1 {
		flagLogOutput = "<stdout>"
	} else {
		if logHandler, err = logging.FileHandler(flagLogOutput, logging.JsonFormat()); err != nil {
			common.PrintFlagsError(runCmd, "--log-output", err)
		}
	}

	log = logging.New("module", "main")
	log.SetHandler(logging.LvlFilterHandler(logLevel, logHandler))
	sebak.SetLogging(logLevel, logHandler)

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

	log.Debug("parsed flags:", parsedFlags...)

	// check node status
	http2Client, _ := sebakcommon.NewHTTP2Client(
		3*time.Second,
		3*time.Second,
		false,
	)
	client := sebaknetwork.NewHTTP2NetworkClient(sebakEndpoint, http2Client)
	if _, err := client.GetNodeInfo(); err != nil {
		common.PrintFlagsError(runCmd, "--sebak-endpoint", err)
	}

	if flagVerbose {
		http2.VerboseLogs = true
		verbose = true
	}
}

func run() {
	server := &http.Server{Addr: flagBind}
	server.SetKeepAlivesEnabled(false)

	http2.ConfigureServer(server, &http2.Server{})

	handler := &Handler{
		kp:            kp,
		sebakEndpoint: sebakEndpoint,
		networkID:     []byte(flagNetworkID),
	}
	router := mux.NewRouter()
	router.HandleFunc("/account/{address}", handler.accountHandler).Methods("POST")
	server.Handler = handlers.CombinedLoggingHandler(os.Stdout, router)

	log.Crit("something wrong", "error", server.ListenAndServeTLS(flagTLSCertFile, flagTLSKeyFile))

	return
}
