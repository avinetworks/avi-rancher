package main

import (
	"time"
	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
        "github.com/gorilla/mux"
        "net/http"
	"os"
)

const (
	metadataUrl = "http://rancher-metadata/2016-07-29"
)

var (
        router          = mux.NewRouter()
        healthcheckPort = ":1000"
	p = new(Avi)
	m metadata.Client
)
var log = logrus.New()
type Avi struct {
	aviSession *AviSession
	cfg        *AviConfig
	cloudRef   string
}

func startHealthcheck() {
        router.HandleFunc("/", healthcheck).Methods("GET", "HEAD").Name("Healthcheck")
        log.Info("Healthcheck handler is listening on ", healthcheckPort)
        log.Fatal(http.ListenAndServe(healthcheckPort, router))
}

func healthcheck(w http.ResponseWriter, req *http.Request) {
        // 1) test metadata server
        _, err := m.GetSelfStack()
        if err != nil {
                log.Errorf("Metadata health check failed: %v", err)
        } else {
                // 2) test Avi
                if err := p.HealthCheck(); err != nil {
                        log.Errorf("Provider health check failed: %v", err)
                } else {
                        w.Write([]byte("OK"))
                }
        }
}

func initLogger() {
	f, err := os.OpenFile("/var/log/avi-rancher.log", os.O_APPEND | os.O_CREATE | os.O_RDWR, 0666)
	if err != nil {
		log.Info("error opening file: %v", err)
	}
	log.Out = f
}

func (p *Avi) Init() error {
	cfg, err := GetAviConfig()
	if err != nil {
		return err
	}

	aviSession, err := InitAviSession(cfg)
	if err != nil {
		return err
	}

	cloudRef, err := aviSession.GetCloudRef(cfg.cloudName)
	if err != nil {
		return err
	}

	p.cfg = cfg
	p.aviSession = aviSession
	p.cloudRef = cloudRef
	log.Info("Avi configuration OK")

	// Initialize metadata client
	log.Info("Initializing Rancher metadata client")
	m, err = metadata.NewClientAndWait(metadataUrl)

	if err != nil {
		log.Fatalf("Failed to initialize Rancher metadata client: %v", err)
		return err
	}

	go startHealthcheck()

	version := "init"
	lastUpdated := time.Now()
	tasks := make(map[string]*Vservice)
	for {
		update := false
		newVersion, err := m.GetVersion()
		if err != nil {
			log.Errorf("Error reading metadata version: %v", err)
		} else if version != newVersion {
			log.Infof("Metadata Version has been changed. Old version: %s. New version: %s.", version, newVersion)
			version = newVersion
			update = true
		} else {
			log.Info("No changes in metadata version")
			if time.Since(lastUpdated).Seconds() >= 30 {
				log.Info("No changes in metadata version last 30 seconds")
				update = true
			}
		}
		if update {
			tasks, err = GetMetadataServiceConfigs(m, p.cfg)
			if err != nil {
				log.Errorf("Failed to get Service configs from metadata: %v", err)
				continue
			}
			parse_docker_tasks(p, tasks)
			lastUpdated = time.Now()
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

func (p *Avi) GetName() string {
	return ProviderName
}

func (p *Avi)HealthCheck() error {
	cloudName := p.cfg.cloudName
	_, err := p.aviSession.GetCloudRef(cloudName)
	if err != nil {
		log.Errorf("Avi Health check failed with error: %s", err)
	}
	return nil
}

func main() {
	initLogger()
	p.Init()
}
