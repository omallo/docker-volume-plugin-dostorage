package main

import (
	"fmt"
	"log/syslog"
	"os"

	"github.com/Sirupsen/logrus"
	logrus_syslog "github.com/Sirupsen/logrus/hooks/syslog"
	"github.com/digitalocean/go-metadata"
	"github.com/docker/go-plugins-helpers/volume"
	flag "github.com/ogier/pflag"
)

const (
	DefaultBaseMountPath   = "/mnt/dostorage"
	DefaultUnixSocketGroup = "root"
)

type CommandLineArgs struct {
	accessToken     *string
	mountPath       *string
	unixSocketGroup *string
	version         *bool
}

func main() {
	configureLogging()

	args := parseCommandLineArgs()

	doMetadataClient := metadata.NewClient()
	doAPIClient := NewDoAPIClient(*args.accessToken)
	doFacade := NewDoFacade(doMetadataClient, doAPIClient)

	mountUtil := NewMountUtil()

	driver, derr := NewDriver(doFacade, mountUtil, *args.mountPath)
	if derr != nil {
		logrus.Fatalf("failed to create the driver: %v", derr)
		os.Exit(1)
	}

	handler := volume.NewHandler(driver)

	serr := handler.ServeUnix(*args.unixSocketGroup, DriverName)
	if serr != nil {
		logrus.Fatalf("failed to bind to the Unix socket: %v", serr)
		os.Exit(1)
	}

	for {
		// block while requests are served in a separate routine
	}
}

func configureLogging() {
	syslogHook, herr := logrus_syslog.NewSyslogHook("", "", syslog.LOG_INFO, DriverName)
	if herr == nil {
		logrus.AddHook(syslogHook)
	} else {
		logrus.Warn("it was not possible to activate logging to the local syslog")
	}
}

func parseCommandLineArgs() *CommandLineArgs {
	args := &CommandLineArgs{}

	args.accessToken = flag.StringP("access-token", "t", "", "the DigitalOcean API access token")
	args.mountPath = flag.StringP("mount-path", "m", DefaultBaseMountPath, "the path under which to create the volume mount folders")
	args.unixSocketGroup = flag.StringP("unix-socket-group", "g", DefaultUnixSocketGroup, "the group to assign to the Unix socket file")
	args.version = flag.Bool("version", false, "outputs the driver version and exits")
	flag.Parse()

	if *args.version {
		fmt.Printf("%v\n", DriverVersion)
		os.Exit(0)
	}

	if *args.accessToken == "" {
		flag.Usage()
		os.Exit(1)
	}

	return args
}
