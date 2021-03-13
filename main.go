package main

import (
	"github.com/containerssh/configuration"
	"github.com/containerssh/http"
	"github.com/containerssh/log"
	"github.com/containerssh/service"
	"github.com/containerssh/structutils"
	"flag"
	"io/ioutil"
	"path/filepath"
	"sigs.k8s.io/yaml"
	"os"
	"github.com/golang/glog"
	"strings"
	"net"
	"fmt"
	"context"
	"os/signal"
	"syscall"
	"time"
)


type Config struct {
	UserFolders []string `json:"userFolders"`
	Users map[string]User `json:"users"`
	PropertiesFolders []string `json:"propertiesFolders"`
	Properties map[string]map[string]interface{} `json:"properties"`
	Server http.ServerConfiguration `json:"server"`
	Log log.Config
}

type User struct {
	Groups		[]string `json:groups,omitempty`
}

type myConfigReqHandler struct {
}

var (
	cfg = &Config{}
	configFlag string
	tmpDir string
	logger log.Logger
)

func (m *myConfigReqHandler) OnConfig(
    request configuration.ConfigRequest,
) (config configuration.AppConfig, err error) {


	appConfig := &configuration.AppConfig{}
	user, ok := cfg.Users[request.Username]
	if !ok {
		return *appConfig, fmt.Errorf("User not exist")
	}

	for _, group := range user.Groups {
		filePath := filepath.Join(tmpDir, group + ".yml")
		if _, err := os.Stat(filePath); err == nil {
			file, err := os.Open(filePath)
			if err != nil {
				logger.Error(
					log.Wrap(
						err,
						"GroupConfigNotFound",
						"Can not open Config file",
					).Label("file", filePath).
					Label("group", group),
				)
				return *appConfig, fmt.Errorf("Group(%s) config not found", group)
			}
			loader, err := configuration.NewReaderLoader(
				file,
				logger,
				configuration.FormatYAML,
			)
			if err != nil {
				logger.Error(
					log.Wrap(
						err,
						"ConfigLoadError",
						"Can not load config for Group",
					).Label("file", filePath).
					Label("group", group),
				)
				return *appConfig, fmt.Errorf("Config Load for Group(%s) not correct", group)
			}

			err = loader.Load(context.Background(), appConfig)
			if err != nil {
				logger.Error(
					log.Wrap(
						err,
						"ConfigParseError",
						"Can not parse config for Group",
					).Label("file", filePath).
					Label("group", group),
				)
				return *appConfig, fmt.Errorf("Parse config for Group(%s) not correct", group)
			}
		} else {
			logger.Info(
				log.Wrap(
					err,
					"FileNotExist",
					"file not Exist",
				).Label("file", filePath).
				Label("group", group),
			)
		}
	}
	return *appConfig, nil
}


func checkIp(remoteIp string, ips []string) bool {
	if len(ips) == 0 { return true }
	    ip := net.ParseIP(remoteIp)
	for _, cidr := range ips {
		_, subnet, error := net.ParseCIDR(cidr)
		if error != nil { return false }
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}


func main() {
	server, err := configuration.NewServer(
		cfg.Server,
		&myConfigReqHandler{},
		logger,
	)

	if err != nil {
	    // Handle error
	}

	lifecycle := service.NewLifecycle(server)

	go func() {
		_ = lifecycle.Run()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if _, ok := <-signals; ok {
			// ok means the channel wasn't closed, let's trigger a shutdown.
			stopContext, _ := context.WithTimeout(
				context.Background(),
				20 * time.Second,
			)
			lifecycle.Stop(stopContext)
		}
	}()
	// Wait for the service to terminate.
	err = lifecycle.Wait()
	// We are already shutting down, ignore further signals
	signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
	// close signals channel so the signal handler gets terminated
	close(signals)

	if err != nil {
	    // Exit with a non-zero signal
	    fmt.Fprintf(
	        os.Stderr,
	        "an error happened while running the server (%v)",
	        err,
	    )
	    os.Exit(1)
	}
	os.Exit(0)
}

func convertMapToFile(){
	tmpDirLocal, err := ioutil.TempDir("", "config");
	if err != nil {
		logger.Error(
			log.Wrap(
				err,
				"TempDirCreateError",
				"Can not Create tmp dir",
			).Label("dir", tmpDirLocal),
		)
	}
	defer os.RemoveAll(tmpDir)
	tmpDir = tmpDirLocal
	for propertiesKey, propertiesValue := range cfg.Properties {
		config, ok := propertiesValue["config"]
		if !ok {
			continue
		}

		content, err := yaml.Marshal(config)
		if err != nil {
			logger.Error(
				log.Wrap(
					err,
					"ParseError",
					"Config file %s can not parsed",
				).Label("File", config),
			)
			os.Exit(-1)
		}
		filePath := filepath.Join(tmpDir, propertiesKey + ".yml")
		f, err := os.Create(filePath)
		if err != nil {
			logger.Error(
				log.Wrap(
					err,
					"FileCreateError",
					"Can not create file",
				).Label("file",filePath),
			)
			os.Exit(-1)
		}
		defer f.Close()

		_, err = f.Write(content);
		if err != nil {
			logger.Error(
				log.Wrap(
					err,
					"FileWriteError",
					"Can not write file",
				).Label("file",filePath),
			)
			os.Exit(-1)
		}
		f.Close()

		file, err := os.Open(filePath)
		loader, err := configuration.NewReaderLoader(
			file,
			logger,
			configuration.FormatYAML,
		)
		if err != nil {
			logger.Error(
				log.Wrap(
					err,
					"ConfigLoaderError",
					"Config load failed",
				).Label("file", filePath),
			)
			os.Exit(-1)
		}

		appConfig := &configuration.AppConfig{}

		err = loader.Load(context.Background(), appConfig);
		if err != nil {
			logger.Error(
				log.Wrap(
					err,
					"ConfigParseError",
					"Config parse failed",
				).Label("file", filePath).
				Label("group", propertiesKey),
			)
			os.Exit(-1)
		}
		file.Close()
	}

}

func convertFileToMap() {
	for _, path := range cfg.UserFolders {
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			user := User{}
			if filepath.Ext(info.Name()) == ".yml" {
				yamlFile, err := ioutil.ReadFile(configFlag)
				if err != nil {
					glog.Fatalf("Cannot get config file %s Get err   #%v ", path, err)
					return err
				}
				err = yaml.Unmarshal(yamlFile, &user)
				if err != nil {
					glog.Fatalf("Config parse error: %s", err)
					return err
				}
				cfg.Users[strings.TrimSuffix(info.Name(), ".yml")] = user
			}
			return nil
		})
		if err != nil {
			os.Exit(-1)
		}
	}

	for _, path := range cfg.PropertiesFolders {
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			properties := map[string]interface{}{}
			if filepath.Ext(info.Name()) == ".yml" {
				yamlFile, err := ioutil.ReadFile(configFlag)
				if err != nil {
					glog.Fatalf("Cannot get config file %s Get err   #%v ", path, err)
					return err
				}
				err = yaml.Unmarshal(yamlFile, &properties)
				if err != nil {
					glog.Fatalf("Config parse error: %s", err)
					return err
				}
				cfg.Properties[strings.TrimSuffix(info.Name(), ".yml")] = properties
			}
			return nil
		})
		if err != nil {
			os.Exit(-1)
		}
	}
}

func init() {

	flag.StringVar(&configFlag, "config", "", "configFile")

	flag.Parse()

	if configFlag != "" {
		yamlFile, err := ioutil.ReadFile(configFlag)
		if err != nil {
			glog.Fatalf("Cannot get config file %s Get err   #%v ", configFlag, err)
			os.Exit(-1)
		}
		if err != nil {
			glog.Fatalf("Config parse error: %v", err)
			os.Exit(-1)
		}
		err = yaml.Unmarshal(yamlFile,&cfg)
		if err != nil {
			glog.Fatalf("Config parse error: %v", err)
			os.Exit(-1)
		}
	}else{
		glog.Fatalf("Need a config file")
		os.Exit(-1)
	}

	if cfg == nil {
		glog.Fatalf("Config file can not be empty")
		os.Exit(-1)
	}


	if cfg.UserFolders == nil { cfg.UserFolders = []string{} }
	if cfg.Users == nil { cfg.Users = make(map[string]User) }

	structutils.Defaults(&cfg.Log)
	structutils.Defaults(&cfg.Server)

	loggerLocal, err :=  log.NewLogger(cfg.Log)
	if err != nil {
		panic("Create Logger failed")
	}
	logger = loggerLocal
	convertFileToMap()
	convertMapToFile()

}
