/*
 * Copyright (C) 2019 Intel Corporation
 * SPDX-License-Identifier: BSD-3-Clause
 */
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"os/signal"
	"strings"
	"strconv"
	"syscall"
	"time"
	stdlog "log"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"intel/isecl/lib/common/middleware"
	"intel/isecl/lib/common/setup"
	"intel/isecl/lib/common/validation"
	"intel/isecl/svs/config"
	"intel/isecl/svs/constants"
	"intel/isecl/svs/resource"
	"intel/isecl/svs/tasks"
	"intel/isecl/svs/version"
	"intel/isecl/lib/common/crypt"
	e "intel/isecl/lib/common/exec"
	cos "intel/isecl/lib/common/os"
	commLog "intel/isecl/lib/common/log"
	commLogMsg "intel/isecl/lib/common/log/message"
	commLogInt "intel/isecl/lib/common/log/setup"
)

type App struct {
	HomeDir        string
	ConfigDir      string
	LogDir         string
	ExecutablePath string
	ExecLinkPath   string
	RunDirPath     string
	Config         *config.Configuration
	ConsoleWriter  io.Writer
	LogWriter      io.Writer
	HTTPLogWriter  io.Writer
	SecLogWriter   io.Writer
}

func (a *App) printUsage() {
	w := a.consoleWriter()
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "    svs <command> [arguments]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Avaliable Commands:")
	fmt.Fprintln(w, "    -h|--help			Show this help message")
	fmt.Fprintln(w, "    setup [task]		Run setup task")
	fmt.Fprintln(w, "    start			Start svs")
	fmt.Fprintln(w, "    status			Show the status of svs")
	fmt.Fprintln(w, "    stop			Stop svs")
	fmt.Fprintln(w, "    tlscertsha384		Show the SHA384 of the certificate used for TLS")
	fmt.Fprintln(w, "    uninstall [--purge]	Uninstall SVS. --purge option needs to be applied to remove configuration and data files")
	fmt.Fprintln(w, "    -v|--version		Show the version of svs")
	fmt.Fprintln(w, "")
        fmt.Fprintln(w, "Setup command usage:     scs setup [task] [--arguments=<argument_value>] [--force]")
        fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Avaliable Tasks for setup:")
        fmt.Fprintln(w, "                              Required env variables:")
        fmt.Fprintln(w, "                                  - get required env variables from all the setup tasks")
        fmt.Fprintln(w, "                              Optional env variables:")
        fmt.Fprintln(w, "                                  - get optional env variables from all the setup tasks")
        fmt.Fprintln(w, "")
        fmt.Fprintln(w, "    svs setup server [--port=<port>]")
        fmt.Fprintln(w, "        - Setup http server on <port>")
        fmt.Fprintln(w, "        - Environment variable SVS_PORT=<port> can be set alternatively")
        fmt.Fprintln(w, "    svs setup tls [--force] [--host_names=<host_names>]")
        fmt.Fprintln(w, "        - Use the key and certificate provided in /etc/threat-detection if files exist")
        fmt.Fprintln(w, "        - Otherwise create its own self-signed TLS keypair in /etc/svs for quality of life")
        fmt.Fprintln(w, "        - Option [--force] overwrites any existing files, and always generate self-signed keypair")
        fmt.Fprintln(w, "        - Argument <host_names> is a list of host names used by local machine, seperated by comma")
        fmt.Fprintln(w, "        - Environment variable SVS_TLS_HOST_NAMES=<host_names> can be set alternatively")
        fmt.Fprintln(w, "    svs setup admin [--user=<username>] [--pass=<password>]")
        fmt.Fprintln(w, "        - Environment variable SVS_ADMIN_USERNAME=<username> can be set alternatively")
        fmt.Fprintln(w, "        - Environment variable SVS_ADMIN_PASSWORD=<password> can be set alternatively")
        fmt.Fprintln(w, "    svs setup reghost [--user=<username>] [--pass=<password>]")
        fmt.Fprintln(w, "        - Environment variable SVS_REG_HOST_USERNAME=<username> can be set alternatively")
        fmt.Fprintln(w, "        - Environment variable SVS_REG_HOST_PASSWORD=<password> can be set alternatively")
        fmt.Fprintln(w, "")
	fmt.Fprintln(w, "    download_ca_cert      Download CMS root CA certificate")
	fmt.Fprintln(w, "                          - Option [--force] overwrites any existing files, and always downloads new root CA cert")
	fmt.Fprintln(w, "                          Required env variables specific to setup task are:")
	fmt.Fprintln(w, "                              - CMS_BASE_URL=<url>                                : for CMS API url")
	fmt.Fprintln(w, "                              - CMS_TLS_CERT_SHA384=<CMS TLS cert sha384 hash>    : to ensure that AAS is talking to the right CMS instance")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "    download_cert TLS     Generates Key pair and CSR, gets it signed from CMS")
	fmt.Fprintln(w, "                          - Option [--force] overwrites any existing files, and always downloads newly signed TLS cert")
	fmt.Fprintln(w, "                          Required env variable if SCS_NOSETUP=true or variable not set in config.yml:")
	fmt.Fprintln(w, "                              - CMS_TLS_CERT_SHA384=<CMS TLS cert sha384 hash>      : to ensure that AAS is talking to the right CMS instance")
	fmt.Fprintln(w, "                          Required env variables specific to setup task are:")
	fmt.Fprintln(w, "                              - CMS_BASE_URL=<url>               : for CMS API url")
	fmt.Fprintln(w, "                              - BEARER_TOKEN=<token>             : for authenticating with CMS")
	fmt.Fprintln(w, "                              - SAN_LIST=<san>                   : list of hosts which needs access to service")
	fmt.Fprintln(w, "                          Optional env variables specific to setup task are:")
	fmt.Fprintln(w, "                              - KEY_PATH=<key_path>              : Path of file where TLS key needs to be stored")
	fmt.Fprintln(w, "                              - CERT_PATH=<cert_path>            : Path of file/directory where TLS certificate needs to be stored")
	fmt.Fprintln(w, "")
}

func (a *App) consoleWriter() io.Writer {
	if a.ConsoleWriter != nil {
		return a.ConsoleWriter
	}
	return os.Stdout
}

func (a *App) logWriter() io.Writer {
	if a.LogWriter != nil {
		return a.LogWriter
	}
	return os.Stderr
}

func (a *App) httpLogWriter() io.Writer {
	if a.HTTPLogWriter != nil {
		return a.HTTPLogWriter
	}
	return os.Stderr
}

func (a *App) configuration() *config.Configuration {
	if a.Config != nil {
		return a.Config
	}
	return config.Global()
}

func (a *App) executablePath() string {
	if a.ExecutablePath != "" {
		return a.ExecutablePath
	}
	exec, err := os.Executable()
	if err != nil {
		log.WithError(err).Error("app:executablePath() Unable to find SVS executable")
		// if we can't find self-executable path, we're probably in a state that is panic() worthy
		panic(err)
	}
	return exec
}

func (a *App) homeDir() string {
	if a.HomeDir != "" {
		return a.HomeDir
	}
	return constants.HomeDir
}

func (a *App) configDir() string {
	if a.ConfigDir != "" {
		return a.ConfigDir
	}
	return constants.ConfigDir
}

func (a *App) logDir() string {
	if a.LogDir != "" {
		return a.ConfigDir
	}
	return constants.LogDir
}

func (a *App) execLinkPath() string {
	if a.ExecLinkPath != "" {
		return a.ExecLinkPath
	}
	return constants.ExecLinkPath
}

func (a *App) runDirPath() string {
	if a.RunDirPath != "" {
		return a.RunDirPath
	}
	return constants.RunDirPath
}

var log = commLog.GetDefaultLogger()
var slog = commLog.GetSecurityLogger()

func (a *App) configureLogs(stdOut, logFile bool) {
	var ioWriterDefault io.Writer
	ioWriterDefault = a.LogWriter
	if stdOut {
		if logFile {
			ioWriterDefault = io.MultiWriter(os.Stdout, a.LogWriter)
	} else {
			ioWriterDefault = os.Stdout
		}
	}

	ioWriterSecurity := io.MultiWriter(ioWriterDefault, a.SecLogWriter)

	f := commLog.LogFormatter{MaxLength: a.configuration().LogMaxLength}
	commLogInt.SetLogger(commLog.DefaultLoggerName, a.configuration().LogLevel, &f, ioWriterDefault, false)
	commLogInt.SetLogger(commLog.SecurityLoggerName, a.configuration().LogLevel, &f, ioWriterSecurity, false)

	slog.Info(commLogMsg.LogInit)
	log.Info(commLogMsg.LogInit)
}

func (a *App) Run(args []string) error {

	if len(args) < 2 {
		a.printUsage()
		os.Exit(1)
	}

	cmd := args[1]
	switch cmd {
	default:
		a.printUsage()
		return errors.New("Unrecognized command: " + args[1])
	case "list":
                if len(args) < 3 {
                        a.printUsage()
                        os.Exit(1)
                }
                return a.PrintDirFileContents(args[2])
	case "tlscertsha384":
		a.configureLogs(false, true)
		hash, err := crypt.GetCertHexSha384(config.Global().TLSCertFile)
		if err != nil {
			fmt.Println(err.Error())
			return err
		}
		fmt.Println(hash)
		return nil
	case "run":
		a.configureLogs(config.Global().LogEnableStdout, true)
		if err := a.startServer(); err != nil {
			fmt.Fprintln(os.Stderr, "Error: daemon did not start - ", err.Error())
			// wait some time for logs to flush - otherwise, there will be no entry in syslog
			time.Sleep(10 * time.Millisecond)
			return err
		}
	case "-h", "--help":
		a.printUsage()
	case "start":
		a.configureLogs(false, true)
		return a.start()
	case "stop":
		a.configureLogs(false, true)
		return a.stop()
	case "status":
		a.configureLogs(false, true)
		return a.status()
	case "uninstall":
		var purge bool
		flag.CommandLine.BoolVar(&purge, "purge", false, "purge config when uninstalling")
		flag.CommandLine.Parse(args[2:])
		a.uninstall(purge)
		os.Exit(0)
	case "--version", "-v":
		fmt.Fprintf(a.consoleWriter(), "SGX Verification Service %s-%s\nBuilt %s\n", version.Version, version.GitHash, version.BuildDate)
	case "setup":
		a.configureLogs(false, true)
		var context setup.Context
		if len(args) <= 2 {
			a.printUsage()
			os.Exit(1)
		}
		if args[2] != "admin" &&
			args[2] != "download_ca_cert" &&
			args[2] != "download_cert" &&
			args[2] != "server" &&
			args[2] != "all" &&
			args[2] != "tls" {
			a.printUsage()
			return errors.New("No such setup task")
		}

		err := validateSetupArgs(args[2], args[3:])
		if err != nil {
			return err
		}

		a.Config = config.Global()
		err = a.Config.SaveConfiguration(context)
		if err != nil {
			fmt.Println("Error saving configuration: " + err.Error())
			os.Exit(1)
		}
		task := strings.ToLower(args[2])
		flags := args[3:]

		if args[2] == "download_cert" && len(args) > 3 {
			flags = args[4:]
		}

		a.Config = config.Global()
		setupRunner := &setup.Runner{
			Tasks: []setup.Task{
				setup.Download_Ca_Cert{
                                        Flags:         args,
					CmsBaseURL:    a.Config.CMSBaseUrl,
                                        CaCertDirPath: constants.TrustedCAsStoreDir,
					TrustedTlsCertDigest: a.Config.CmsTlsCertDigest,
                                        ConsoleWriter: os.Stdout,
                                },
				setup.Download_Cert{
					Flags:              flags,
					KeyFile:            a.Config.TLSKeyFile,
					CertFile:           a.Config.TLSCertFile,
					KeyAlgorithm:       constants.DefaultKeyAlgorithm,
					KeyAlgorithmLength: constants.DefaultKeyAlgorithmLength,
					CmsBaseURL:         a.Config.CMSBaseUrl,
					Subject: pkix.Name{
						CommonName:   a.Config.Subject.TLSCertCommonName,
					},
					SanList:	a.Config.CertSANList,
					CertType:      "TLS",
					CaCertsDir:    constants.TrustedCAsStoreDir,
					BearerToken:   "",
					ConsoleWriter: os.Stdout,
				},
				tasks.Server{
					Flags:         flags,
					Config:        a.configuration(),
					ConsoleWriter: os.Stdout,
				},
			},
			AskInput: false,
		}
		if task == "all" {
			err = setupRunner.RunTasks()
		} else {
			err = setupRunner.RunTasks(task)
		}
		if err != nil {
			log.WithError(err).Error("Error running setup")
			fmt.Println("Error running setup: ", err)
			return err
		}
		svsUser, err := user.Lookup(constants.SVSUserName)
		if err != nil {
			return errors.Wrapf(err,"Could not find user '%s'", constants.SVSUserName)
		}

		uid, err := strconv.Atoi(svsUser.Uid)
		if err != nil {
			return errors.Wrapf(err,"Could not parse scs user uid '%s'", svsUser.Uid)
		}

		gid, err := strconv.Atoi(svsUser.Gid)
		if err != nil {
			return errors.Wrapf(err,"Could not parse scs user gid '%s'", svsUser.Gid)
		}

		//Change the fileownership to scs user

		err = cos.ChownR(constants.ConfigDir, uid, gid)
		if err != nil {
			return errors.Wrap(err,"Error while changing file ownership")
		}
		if task == "download_cert" {
			err = os.Chown(a.Config.TLSKeyFile, uid, gid)
			if err != nil {
				return errors.Wrap(err, "Error while changing ownership of TLS Key file")
			}

			err = os.Chown(a.Config.TLSCertFile, uid, gid)
			if err != nil {
				return errors.Wrap(err, "Error while changing ownership of TLS Cert file")
			}
		}
	}
	return nil
}

func (a *App) startServer() error {
	c := a.configuration()
	log.Info("Starting SVS Server")
	// Create Router, set routes
	r := mux.NewRouter()
	r.SkipClean(true)
	sr := r.PathPrefix("/svs/v1/").Subrouter()
	sr.Use(middleware.NewTokenAuth(constants.TrustedJWTSigningCertsDir,
					constants.TrustedCAsStoreDir, fnGetJwtCerts,
					constants.DefaultJwtValidateCacheKeyMins))
	func(setters ...func(*mux.Router,*config.Configuration)) {
		for _, setter := range setters {
			setter(sr,c)
		}
	}(resource.QuoteVerifyCB)

	tlsconfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
	}
	// Setup signal handlers to gracefully handle termination
	stop := make(chan os.Signal)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	httpLog := stdlog.New(a.httpLogWriter(), "", 0)
	h := &http.Server{
		Addr:      fmt.Sprintf(":%d", c.Port),
		Handler:   handlers.RecoveryHandler(handlers.RecoveryLogger(httpLog), handlers.PrintRecoveryStack(true))(handlers.CombinedLoggingHandler(a.httpLogWriter(), r)),
		ErrorLog:  httpLog,
		TLSConfig: tlsconfig,
		ReadTimeout:       c.ReadTimeout,
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		MaxHeaderBytes:    c.MaxHeaderBytes,
	}

	// dispatch web server go routine
	go func() {
		tlsCert := config.Global().TLSCertFile
		tlsKey := config.Global().TLSKeyFile
		if err := h.ListenAndServeTLS(tlsCert, tlsKey); err != nil {
			log.WithError(err).Info("Failed to start HTTPS server")
			stop <- syscall.SIGTERM
		}
	}()

	slog.Info(commLogMsg.ServiceStart)
	// TODO dispatch Service status checker goroutine
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		log.WithError(err).Info("Failed to gracefully shutdown webserver")
		return err
	}
	slog.Info(commLogMsg.ServiceStop)
	return nil
}

func (a *App) start() error {
	fmt.Fprintln(a.consoleWriter(), `Forwarding to "systemctl start svs"`)
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.Wrap(err, "app:start() Could not locate systemctl to start application service")
	}
	return syscall.Exec(systemctl, []string{"systemctl", "start", "svs"}, os.Environ())
}

func (a *App) stop() error {
	fmt.Fprintln(a.consoleWriter(), `Forwarding to "systemctl stop svs"`)
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.Wrap(err, "app:stop() Could not locate systemctl to stop application service")
	}
	return syscall.Exec(systemctl, []string{"systemctl", "stop", "svs"}, os.Environ())
}

func (a *App) status() error {
	fmt.Fprintln(a.consoleWriter(), `Forwarding to "systemctl status svs"`)
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.Wrap(err, "app:status() Could not locate systemctl to check status of application service")
	}
	return syscall.Exec(systemctl, []string{"systemctl", "status", "svs"}, os.Environ())
}

func (a *App) uninstall(purge bool) {
	fmt.Println("Uninstalling sgx Verification Service")
	removeService()

	fmt.Println("removing : ", a.executablePath())
	err := os.Remove(a.executablePath())
	if err != nil {
		log.WithError(err).Error("error removing executable")
	}

	fmt.Println("removing : ", a.runDirPath())
	err = os.Remove(a.runDirPath())
	if err != nil {
		log.WithError(err).Error("error removing ", a.runDirPath())
	}
	fmt.Println("removing : ", a.execLinkPath())
	err = os.Remove(a.execLinkPath())
	if err != nil {
		log.WithError(err).Error("error removing ", a.execLinkPath())
	}

	if purge {
		fmt.Println("removing : ", a.configDir())
		err = os.RemoveAll(a.configDir())
		if err != nil {
			log.WithError(err).Error("error removing config dir")
		}
	}
	fmt.Println("removing : ", a.logDir())
	err = os.RemoveAll(a.logDir())
	if err != nil {
		log.WithError(err).Error("error removing log dir")
	}
	fmt.Println("removing : ", a.homeDir())
	err = os.RemoveAll(a.homeDir())
	if err != nil {
		log.WithError(err).Error("error removing home dir")
	}
	fmt.Fprintln(a.consoleWriter(), "sgx Verification Service uninstalled")
	a.stop()
}
func removeService() {
	_, _, err := e.RunCommandWithTimeout(constants.ServiceRemoveCmd, 5)
	if err != nil {
		fmt.Println("Could not remove sgx Verification Service")
		fmt.Println("Error : ", err)
	}
}

func validateCmdAndEnv(env_names_cmd_opts map[string]string, flags *flag.FlagSet) error {
	env_names := make([]string, 0)
	for k, _ := range env_names_cmd_opts {
		env_names = append(env_names, k)
	}

	missing, err := validation.ValidateEnvList(env_names)
	if err != nil && missing != nil {
		for _, m := range missing {
			if cmd_f := flags.Lookup(env_names_cmd_opts[m]); cmd_f == nil {
				return errors.New("Insufficient arguments")
			}
		}
	}
	return nil
}

func validateSetupArgs(cmd string, args []string) error {
	var fs *flag.FlagSet

	switch cmd {
	default:
		return errors.New("Unknown command")

	case "download_ca_cert":
		return nil

	case "download_cert":
		return nil

	case "admin":
                env_names_cmd_opts := map[string]string{
                        "SVS_ADMIN_USERNAME": "user",
                        "SVS_ADMIN_PASSWORD": "pass",
                }

                fs = flag.NewFlagSet("admin", flag.ContinueOnError)
                fs.String("user", "", "Username for admin authentication")
                fs.String("pass", "", "Password for admin authentication")

                err := fs.Parse(args)
                if err != nil {
                        return fmt.Errorf("Fail to parse arguments: %s", err.Error())
                }
                return validateCmdAndEnv(env_names_cmd_opts, fs)

	case "server":
		return nil

	case "tls":
		env_names_cmd_opts := map[string]string{
			"SVS_TLS_HOST_NAMES": "host_names",
		}

		fs = flag.NewFlagSet("tls", flag.ContinueOnError)
		fs.String("host_names", "", "comma separated list of hostnames to add to TLS cert")

		err := fs.Parse(args)
		if err != nil {
			return errors.Wrap(err, "Fail to parse arguments")
		}
		return validateCmdAndEnv(env_names_cmd_opts, fs)

	case "all":
		if len(args) != 0 {
			return errors.New("Please setup the arguments with env")
		}
	}
	return nil
}

func (a* App) PrintDirFileContents(dir string) error {
        if dir == "" {
                return fmt.Errorf("PrintDirFileContents needs a directory path to look for files")
        }
        data, err := cos.GetDirFileContents(dir, "")
        if err != nil {
                return err
        }
        for i, fileData := range data {
                fmt.Println("File :", i)
                fmt.Printf("%s",fileData)
        }
        return nil
}

//To be implemented if JWT certificate is needed from any other services
func fnGetJwtCerts() error {
	conf := config.Global()

	if !strings.HasSuffix(conf.AuthServiceUrl, "/") {
		conf.AuthServiceUrl = conf.AuthServiceUrl + "/"
	}
	url := conf.AuthServiceUrl + "noauth/jwt-certificates"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return errors.Wrap(err, "Could not create http request")
	}
	req.Header.Add("accept", "application/x-pem-file")
	rootCaCertPems, err := cos.GetDirFileContents(constants.TrustedCAsStoreDir, "*.pem")
	if err != nil {
		return errors.Wrap(err, "Could not read root CA certificate")
	}

	// Get the SystemCertPool, continue with an empty pool on error
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	for _, rootCACert := range rootCaCertPems {
		if ok := rootCAs.AppendCertsFromPEM(rootCACert); !ok {
			return err
		}
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
				RootCAs:            rootCAs,
			},
		},
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "Could not retrieve jwt certificate")
	}
	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)
	err = crypt.SavePemCertWithShortSha1FileName(body, constants.TrustedJWTSigningCertsDir)
	if err != nil {
		return errors.Wrap(err, "Could not store Certificate")
	}

	return nil
}
