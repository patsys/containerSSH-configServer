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
	"io"
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

type sureFireWriter struct {
	backend io.Writer
}

var (
	cfg = &Config{}
	configFlag string
	tmpDir string
	logger log.Logger
)

func (s *sureFireWriter) Write(p []byte) (n int, err error) {
	n, err = s.backend.Write(p)
	if err != nil {
		// Ignore errors      
		return len(p), nil
	}
	return n, nil
}

func (m *myConfigReqHandler) OnConfig(
    request configuration.ConfigRequest,
) (config configuration.AppConfig, err error) {


	appConfig := &configuration.AppConfig{}
	user, ok := cfg.Users[request.Username]
	if !ok {
		return *appConfig, fmt.Errorf("User not exist")
	}

	for _, group := range user.Groups {
		if _, err := os.Stat(filepath.Join(tmpDir, group + ".yml")); err == nil {
			file, err := os.Open(filepath.Join(tmpDir, group + ".yml"))
			if err != nil {
				logger.Errorf("Can not open Config file: %v", err)
				return *appConfig, fmt.Errorf("Can not open Config file: %v", err)
			}
			loader, err := configuration.NewReaderLoader(
				file,
				logger,
				configuration.FormatYAML,
			)
			if err != nil {
				logger.Errorf("Config Load not Correct: %v", err)
				return *appConfig, fmt.Errorf("Config Load not Correct: %v", err)
			}

			err = loader.Load(context.Background(), appConfig)
			if err != nil {
				logger.Errorf("Config not Correct: %v", err)
				return *appConfig, fmt.Errorf("Config not Correct: %v", err)
			}
		} else {
			logger.Errorf("File %s not exist", filepath.Join(tmpDir, group + ".yml"))
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
	_ = lifecycle.Run()
}

func convertMapToFile(){
	tmpDirLocal, err := ioutil.TempDir("", "config")
	if err != nil {
		logger.Errorf("Can not Create tmp dir: %v", err)
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
			logger.Errorf("Config file %s can not parsed: %v",config, err)
			os.Exit(-1)
		}

		f, err := os.Create(filepath.Join(tmpDir, propertiesKey + ".yml"))
		if err != nil {
			logger.Errorf("Can not create file %s: %v", filepath.Join(tmpDir, propertiesKey + ".yml"), err)
			os.Exit(-1)
		}
		defer f.Close()

		_, err = f.Write(content)
		if err != nil {
			logger.Errorf("Can not write file %s: %v", filepath.Join(tmpDir, propertiesKey + ".yml"), err)
			os.Exit(-1)
		}
		f.Close()

		file, err := os.Open(filepath.Join(tmpDir, propertiesKey + ".yml"))
		 _, err = configuration.NewReaderLoader(
			file,
    		logger,
    		configuration.FormatYAML,
		)
		if err != nil {
			logger.Errorf("Config failed: %v", err)
			os.Exit(-1)
		}		
		appConfig := &configuration.AppConfig{}
		loader, err := configuration.NewReaderLoader(
			file,
			logger,
			configuration.FormatYAML,
		)
		if err != nil {
			logger.Errorf("Config Load not Correct: %v", err)
			os.Exit(-1)
		}
		err = loader.Load(context.Background(), appConfig)
		if err != nil {
			logger.Errorf("Can not parse file group %s: %v",propertiesKey, err)
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

	loggerLocal, err :=  log.NewFactory(&sureFireWriter{os.Stdout}).Make(cfg.Log, "")
	if err != nil {
		panic("Create Logger failed")
	}
	logger = loggerLocal
	convertFileToMap()
	convertMapToFile()

}

