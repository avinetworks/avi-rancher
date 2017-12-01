package main

import (
	"fmt"
	"strconv"
	"crypto/md5"
	"io"
	"strings"
)

const (
	APP_PROFILE_HTTPS           = "System-Secure-HTTP"
	APP_PROFILE_HTTP            = "System-HTTP"
	APP_PROFILE_L4              = "System-L4-Application"
	APP_PROFILE_SSL             = "System-SSL-Application"
	NET_PROFILE_TCP             = "System-TCP-Proxy"
	NET_PROFILE_UDP             = "System-UDP-Fast-Path"
	HEALTH_MONITOR_HTTPS        = "System-HTTPS"
	HEALTH_MONITOR_HTTP         = "System-HTTP"
	HEALTH_MONITOR_TCP          = "System-TCP"
	HEALTH_MONITOR_UDP          = "System-UDP"
	SSL_PROFILE                 = "System-Standard"
	SSL_STANDARD_CERT           = "System-Default-Cert"


	AVI_INTEGRATION_LABEL       = "avi_proxy"
	AVI_SSL_LABEL               = "avi_ssl_key_and_certificate_refs"
	AVI_CLOOUD_REF              = "avi_cloud_ref"
	AVI_FQDN                    = "avi_fqdn"
	AVI_APPLICATION_PROFILE_REF = "avi_application_profile_ref"
)

type Vservice struct {
        serviceName string
        labels      map[string]string // list of lables on services
        pools       []pool // Pool servers in Avi
}

type pool struct {
        protocol    string // tcp/udp
        hostip string // Host IP
        poolName    string
        ports map[int]int // Host to Container port mapping
}

func update_labels_data(task *Vservice, vs map[string]interface{}) map[string]interface{} {
	cloud, ok := task.labels[AVI_CLOOUD_REF]
	if ok {
		vs["cloud_ref"] = "/api/cloud?name="+cloud
		log.Info("Overwriting the service cloud with ", cloud)
	}
	fqdn, ok := task.labels[AVI_FQDN]
	if ok {
		vs["fqdn"] = fqdn
		log.Info("Overwriting the service fqdn with ", fqdn)
	}
	app, ok := task.labels[AVI_APPLICATION_PROFILE_REF]
	if ok {
		vs["application_profile_ref"] = "/api/applicationprofile?name="+app
		log.Info("Overwriting the service application profile with ", app)
		if app == APP_PROFILE_HTTPS {
			vs["ssl_key_and_certificate_refs"] = configure_ssl(task)
			vs["services"] = configure_services(task, vs)
		}
	}
	return vs
}

func CalculateChecksum(task *Vservice) []byte {
	h := md5.New()
	io.WriteString(h, task.serviceName)
	for key, val := range task.labels {
		label := key+":"+val
		io.WriteString(h, label)
	}
	for _, val := range task.pools {
		io.WriteString(h, val.protocol)
		io.WriteString(h, val.hostip)
		io.WriteString(h, val.poolName)
		for publicport, privateport := range val.ports {
			io.WriteString(h, strconv.Itoa(publicport))
			io.WriteString(h, strconv.Itoa(privateport))
		}
	}
	return h.Sum(nil)
}

func configure_vip() []map[string]interface{} {
	var slice []map[string]interface{}
	vip := make(map[string]interface{})
	vip["auto_allocate_ip"] = true
	slice = append(slice, vip)
	return slice
}

func configure_fqdn(relname string, subdomain string) string {
	if subdomain != "" {
		fqdn := relname+"."+subdomain
		return fqdn
	}
	return ""
}

func configure_app_net_profile(task *Vservice) (string, string, []string) {
	var app, net string
	var ssl_certs []string
	for _, pool := range task.pools {
		for _, privateport := range pool.ports {
			if privateport == 443 {
				app = "/api/applicationprofile?name="+APP_PROFILE_HTTPS
				ssl_certs = configure_ssl(task)
				return app, "", ssl_certs
			} else if privateport == 80 {
				app = "/api/applicationprofile?name="+APP_PROFILE_HTTP
				return app, "", ssl_certs
			} else {
				app = "/api/applicationprofile?name="+APP_PROFILE_L4
				if pool.protocol == "tcp" {
					net = "/api/networkprofile?name="+NET_PROFILE_TCP
				} else {
					net = "/api/networkprofile?name="+NET_PROFILE_UDP
				}
				return app, net, ssl_certs
			}
		}
	}
	return "", "", ssl_certs
}

func configure_services(task *Vservice, vs map[string]interface{}) []map[string]interface{} {
	var s []map[string]interface{}
	for _, pool := range task.pools {
		for _, privateport := range pool.ports {
			found := false
			for _, ex_port := range s {
				if ex_port["port"] == privateport {
					found = true
					break
				}
			}
			if !found {
				services := make(map[string]interface{})
				services["port"] = privateport
				if vs["application_profile_ref"] == "/api/applicationprofile?name="+APP_PROFILE_HTTPS {
					services["enable_ssl"] = true
					services["port"] = 443
				}
				s = append(s, services)
			}
		}
	}
	return s
}

func configure_ssl(task *Vservice) ([]string) {
	var ssl_cert []string
	ssl_label, ok := task.labels[AVI_SSL_LABEL]
	if ok {
		ssl := "/api/sslkeyandcertificate?name="+ssl_label
		ssl_cert = append(ssl_cert, ssl)
	} else {
		ssl := "/api/sslkeyandcertificate?name="+SSL_STANDARD_CERT
		ssl_cert = append(ssl_cert, ssl)
	}	
	return ssl_cert
}

func configure_pool_hms(task *Vservice) ([]string, string) {
	var hm []string
	var hm_ref string
	ssl_prof := ""
	for _, pool := range task.pools {
		for _, privateport := range pool.ports {
			if privateport == 443 {
				hm_ref = "/api/healthmonitor?name="+HEALTH_MONITOR_HTTPS
				ssl_prof = "/api/sslprofile?name="+SSL_PROFILE
			} else if privateport == 80 {
				hm_ref = "/api/healthmonitor?name="+HEALTH_MONITOR_HTTP
			} else {
				if pool.protocol == "tcp" {
					hm_ref = "/api/healthmonitor?name="+HEALTH_MONITOR_TCP
				} else {
					hm_ref = "/api/healthmonitor?name="+HEALTH_MONITOR_UDP
				}
			}
			found := false
			for _, ex_hm := range hm {
				if ex_hm == hm_ref {
					found = true
					break
				}
			}
			if !found {
				hm = append(hm, hm_ref)
			}
		}
	}
	return hm, ssl_prof
}

func configure_pool_servers(task *Vservice) ([]map[string]interface{}, string) {
	var s []map[string]interface{}
	var name string
	for _, pool := range task.pools {
		name = pool.poolName
		for publicport, _ := range pool.ports {
			server := make(map[string]interface{})
			ip := make(map[string]interface{})
			ip["type"] = "V4"
			ip["addr"] = pool.hostip
			server["ip"] = ip
			server["port"] = publicport
			s = append(s, server)
		}
	}
	return s, name
}

func (p *Avi)configure_pool(task *Vservice, create bool, pg map[string]interface{}) map[string]interface{} {
	pool := make(map[string]interface{})
	pool["cloud_ref"] = p.cloudRef
	pool["tenant_ref"], _ = p.aviSession.GetTenantRef(p.cfg.tenant)
	hm_refs, ssl_prof := configure_pool_hms(task)
	if len(hm_refs) > 0 {
		pool["health_monitor_refs"] = hm_refs
	}
	if ssl_prof != "" {
		pool["ssl_profile_ref"] = ssl_prof
	}
	pool["servers"], pool["name"] = configure_pool_servers(task)
	if !create {
		for _, poolmem := range pg["members"].([]interface{}) {
			pool_tokens := strings.Split(poolmem.(map[string]interface{})["pool_ref"].(string), "/")
			pool["uuid"] = pool_tokens[len(pool_tokens)-1]
		}
	}
	return pool
}

func (p *Avi)configure_poolgmembers(task *Vservice, create bool, vs_update map[string]interface{}) ([]map[string]interface{}) {
	var poolg []map[string]interface{}
	var pg map[string]interface{}
	poolgmem := make(map[string]interface{})
	if !create {
		pg_name := fmt.Sprintf("%s-poolgroup", task.serviceName)
		pg, _ = p.GetPoolGroup(pg_name)
	}
	poolgmem["pool_ref_data"] = p.configure_pool(task, create, pg)
	poolg = append(poolg, poolgmem)	
	return poolg
}

func (p *Avi)configure_poolgroup(task *Vservice, create bool, vs_update map[string]interface{}) map[string]interface{} {
	poolg := make(map[string]interface{})
	poolg["cloud_ref"] = p.cloudRef
	poolg["tenant_ref"], _ = p.aviSession.GetTenantRef(p.cfg.tenant)
	poolg["name"] = fmt.Sprintf("%s-poolgroup", task.serviceName)
	poolg["members"] = p.configure_poolgmembers(task, create, vs_update)
	if !create {
		poolg_tokens := strings.Split(vs_update["pool_group_ref"].(string), "/")
		poolg["uuid"] = poolg_tokens[len(poolg_tokens)-1]
	}
	return poolg
}

func (p *Avi) CreateUpdateVS(task *Vservice, create bool, vs_update map[string]interface{}) {
	var resp interface{}
	vs := make(map[string]interface{})
	vs["name"] = task.serviceName
	vs["cloud_ref"] = p.cloudRef
	vs["created_by"] = "Rancher"
	vs["cloud_config_cksum"] = fmt.Sprintf("%x", CalculateChecksum(task))

	vs["vip"] = configure_vip()

	fqdn := configure_fqdn(task.serviceName, p.cfg.dnsSubDomain)
	if fqdn != "" {
		vs["fqdn"] = fqdn
	}

	vs["tenant_ref"], _ = p.aviSession.GetTenantRef(p.cfg.tenant)

	app, net, ssl_certs := configure_app_net_profile(task)
	if app != "" {
		vs["application_profile_ref"] = app
	}
	if net != "" {
		vs["network_profile_ref"] = net
	}
	if len(ssl_certs) > 0 {
		vs["ssl_key_and_certificate_refs"] = ssl_certs
	}

	vs["services"] = configure_services(task, vs)

	vs["pool_group_ref_data"] = p.configure_poolgroup(task, create, vs_update)

	if !create {
		vs["uuid"] = vs_update["uuid"]
	}

	var err error
	model := make(map[string]interface{})
	model["data"] = vs
	model["model_name"] = "VirtualService"

	vs = update_labels_data(task, vs)

	if create {
		resp, err = p.aviSession.Post("/api/macro", model)
	} else {
		resp, err = p.aviSession.Put("/api/macro", model)
	}
	if err != nil {
		log.Infof("Error in creating/updating VS %s: %v", task.serviceName, resp)
	} else {
		log.Infof("VS %s created/Updated %v", task.serviceName, resp)
	}
}

func (p *Avi) DeleteVS(vs map[string]interface{}) {
	var resp interface{}
	var err error
	model := make(map[string]interface{})
	model["data"] = vs
	model["model_name"] = "VirtualService"
	resp, err = p.aviSession.Del("/api/macro", model)
	if err != nil {
		log.Infof("Error deleting VS %s: %v", vs, resp)
	} else {
		log.Infof("VS %s deleted %v", vs["name"], resp)
	}
}
