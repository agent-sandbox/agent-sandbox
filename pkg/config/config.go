/*
 * Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/kelseyhightower/envconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const Version = "0.4.3"

const SystemToken = "sys-2492a85b10ed4cb083b2c76b181eac96"

type Resources struct {
	CPU         string `json:"cpu"`
	Memory      string `json:"memory"`
	CPULimit    string `json:"cpuLimit"`
	MemoryLimit string `json:"memoryLimit"`
}

type TemplatePool struct {
	Size       int       `json:"size"`
	ReadySize  int       `json:"readySize"`
	ProbePort  int       `json:"probePort"`
	WarmupCmd  string    `json:"warmupCmd"`
	StartupCmd string    `json:"startupCmd"`
	Resources  Resources `json:"resources"`
}

type Template struct {
	Name           string            `json:"name" required:"false"`
	Pattern        string            `json:"pattern" required:"false"`
	Image          string            `json:"image" required:"false"`
	Port           int               `json:"port" required:"false"`
	Type           string            `json:"type" required:"false" description:"dynamic or static, default is static, dynamic means template is dynamic by regexp"`
	Metadata       map[string]string `json:"metadata" required:"false"`
	NoStartupProbe bool              `json:"noStartupProbe" required:"false"`
	Args           []string          `json:"args" required:"false"`
	EnvVars        map[string]string `json:"envVars" required:"false"`
	Shell          string            `json:"shell" required:"false"`
	ExtraPorts     []int             `json:"extraPorts" required:"false"`
	Resources      Resources         `json:"resources"  required:"false"`
	Pool           TemplatePool      `json:"pool" required:"false"`
	Description    string            `json:"description" required:"false"`
}

var Cfg *Config
var Templates []*Template
var SandboxDeployTemplate string

type Config struct {
	KubeClient kubernetes.Interface `ignored:"true"`

	APIVersion   string   `split_words:"true" default:"v1" required:"false"`
	APIBaseURL   string   `split_words:"true" default:"" required:"false"`
	ServerAddr   string   `split_words:"true" default:"0.0.0.0:10000" required:"false"`
	APITokensRaw string   `split_words:"true" default:"" required:"false"`
	APITokens    []string `ignored:"true"`

	// witch Kubernetes namespace to create sandboxes Replicaset&Pod in
	SandboxNamespace string `split_words:"true" default:"default" required:"false"`

	SandboxTemplateFile string `split_words:"true" default:"config/sandbox.yaml" required:"false"`

	SandboxTemplatesConfigFile string `split_words:"true" default:"config/templates.json" required:"false"`
	SandboxDefaultImage        string `split_words:"true" default:"ghcr.io/agent-infra/sandbox:latest" required:"false"`
	SandboxDefaultTemplate     string `split_words:"true" default:"aio" required:"false"`

	ConfigmapName string `split_words:"true" default:"agent-sandbox" required:"false"`

	// SandboxProxyDomain enables subdomain-based routing for proxied sandbox web apps.
	// When set (e.g. "sandbox.yourdomain.com"), requests arriving on {name}.{domain}
	// are proxied directly to the sandbox pod without path prefix stripping.
	SandboxProxyDomain string `split_words:"true" default:"" required:"false"`
}

func InitConfig() *Config {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		klog.Fatal("Failed to process config: ", err)
	}

	cfg.APIBaseURL = "/api/" + cfg.APIVersion

	cfg.APITokensRaw = SystemToken + "," + cfg.APITokensRaw
	tokens := strings.Split(cfg.APITokensRaw, ",")
	//valid tokens
	var validTokens []string
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" && len(token) >= 5 {
			validTokens = append(validTokens, token)
		}
	}
	cfg.APITokens = validTokens

	Cfg = &cfg

	return Cfg
}

func (c *Config) ShouldLoadSandboxTemplate() {
	content, err := c.ReadSandboxTemplateFromCM()
	if content == "" {
		klog.Errorf("failed to read sandbox template from configmap, content is empty, error: %v", err)
	}
	klog.Info("loaded sandbox template from configmap content = ", content)
	SandboxDeployTemplate = content
}

func (c *Config) CheckConfigmap() {
	templatesContent, err := c.ReadTemplatesFromCM()
	if templatesContent == "" {
		klog.Info("templates config is empty, will load from local file system")

		fileName := c.SandboxTemplatesConfigFile
		content, readErr := os.ReadFile(fileName)
		if readErr != nil {
			klog.Fatalf("Failed to read Template config file %s error: %v", fileName, readErr)
		}

		templatesContent = string(content)
		klog.Infof("Loaded Template config from file %s", fileName)

		if err = c.SaveTemplatesToCM(templatesContent); err != nil {
			klog.Fatalf("Failed to save Templates config to configmap, error: %v", err)
		}
		klog.Info("Templates config saved to configmap successfully")
	} else {
		klog.Info("templates config already exists in configmap")
	}

	sandboxTemplateContent, err := c.ReadSandboxTemplateFromCM()
	if err != nil {
		klog.Fatalf("Failed to read sandbox template from configmap: %v", err)
	}
	if sandboxTemplateContent == "" {
		klog.Info("sandbox template config is empty, will load from local file system")

		fileName := c.SandboxTemplateFile
		content, readErr := os.ReadFile(fileName)
		if readErr != nil {
			klog.Fatalf("Failed to read sandbox template file %s error: %v", fileName, readErr)
		}

		sandboxTemplateContent = string(content)
		klog.Infof("Loaded sandbox template config from file %s", fileName)

		if err = c.SaveSandboxTemplateToCM(sandboxTemplateContent); err != nil {
			klog.Fatalf("Failed to save sandbox template config to configmap, error: %v", err)
		}
		klog.Info("Sandbox template config saved to configmap successfully")
	} else {
		klog.Info("sandbox template config already exists in configmap")
	}
}

// ShouldLoadTemplates load templates config from:
func (c *Config) ShouldLoadTemplates() {
	templatesContent := ""

	// load config from configmap
	content, err := c.ReadTemplatesFromCM()
	if content == "" {
		klog.Errorf("Failed to read Templates config from configmap, content is empty, error: %v", err)
	}
	klog.Info("Loaded Templates config from configmap: ", content)
	templatesContent = content

	var tpls []*Template
	err = json.Unmarshal([]byte(templatesContent), &tpls)
	if err != nil {
		klog.Errorf("Failed to unmarshal Template config templatesContent %s error: %v", templatesContent, err)
	}

	//check envs not empty
	if len(tpls) == 0 {
		klog.Errorf("No Templates  found in config content %s", templatesContent)
	}

	//varify each env has name  image and description
	for _, env := range tpls {
		if env.Name == "" || env.Image == "" || env.Description == "" {
			klog.Errorf("Invalid Template config in templatesContent %s: %+v, name image and desc must not dempty", templatesContent, env)
		}
	}

	Templates = tpls

	//log loaded envs
	for _, env := range Templates {
		klog.Infof("Loaded Template object: %+v", env)
	}
}

func (c *Config) SaveTemplatesToCM(templatesContent string) error {
	cmClient := c.KubeClient.CoreV1().ConfigMaps(c.SandboxNamespace)

	existCm, err := cmClient.Get(context.TODO(), Cfg.ConfigmapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      Cfg.ConfigmapName,
					Namespace: c.SandboxNamespace,
				},
				Data: map[string]string{
					TemplatesConfigMapKey: templatesContent,
				},
			}
			_, createErr := cmClient.Create(context.TODO(), cm, metav1.CreateOptions{})
			return createErr
		}

		return err
	}

	if existCm.Data == nil {
		existCm.Data = map[string]string{}
	}
	existCm.Data[TemplatesConfigMapKey] = templatesContent
	_, err = cmClient.Update(context.TODO(), existCm, metav1.UpdateOptions{})
	return err
}

func (c *Config) ReadTemplatesFromCM() (content string, err error) {
	templatesContent := ""

	existCm, err := c.KubeClient.CoreV1().ConfigMaps(c.SandboxNamespace).Get(context.TODO(), Cfg.ConfigmapName, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Info("templates configmap not found, return empty content")
		return templatesContent, nil
	} else if err != nil {
		klog.Errorf("Failed to get ConfigMap for Templates config: %v", err)
		return templatesContent, err
	}

	return existCm.Data[TemplatesConfigMapKey], nil
}

func (c *Config) SaveSandboxTemplateToCM(content string) error {
	cmClient := c.KubeClient.CoreV1().ConfigMaps(c.SandboxNamespace)

	existCm, err := cmClient.Get(context.TODO(), Cfg.ConfigmapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      Cfg.ConfigmapName,
					Namespace: c.SandboxNamespace,
				},
				Data: map[string]string{
					SandboxTemplateConfigMapKey: content,
				},
			}
			_, createErr := cmClient.Create(context.TODO(), cm, metav1.CreateOptions{})
			return createErr
		}

		return err
	}

	if existCm.Data == nil {
		existCm.Data = map[string]string{}
	}
	existCm.Data[SandboxTemplateConfigMapKey] = content
	_, err = cmClient.Update(context.TODO(), existCm, metav1.UpdateOptions{})
	return err
}

func (c *Config) ReadSandboxTemplateFromCM() (content string, err error) {
	sandboxTemplateContent := ""

	existCm, err := c.KubeClient.CoreV1().ConfigMaps(c.SandboxNamespace).Get(context.TODO(), Cfg.ConfigmapName, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		klog.Info("sandbox template configmap not found, return empty content")
		return sandboxTemplateContent, nil
	} else if err != nil {
		klog.Errorf("Failed to get ConfigMap for sandbox template config: %v", err)
		return sandboxTemplateContent, err
	}

	return existCm.Data[SandboxTemplateConfigMapKey], nil
}

func GetTemplateByName(name string) (*Template, error) {
	for _, t := range Templates {
		if t.Name == name {
			return t, nil
		}
	}

	for _, t := range Templates {
		if t.Type == "dynamic" {
			image := t.Image
			//match by regexp
			re := regexp.MustCompile(t.Pattern)
			match := re.FindStringSubmatch(name)
			if len(match) == 0 {
				continue
			}

			if len(match) > 0 {
				versionIndex := re.SubexpIndex("version")
				nameIndex := re.SubexpIndex("name")
				if nameIndex == -1 || versionIndex == -1 {
					continue
				}

				tversion := match[versionIndex]
				tname := match[nameIndex]
				image = strings.ReplaceAll(image, "<name>", tname)
				image = strings.ReplaceAll(image, "<version>", tversion)
			}

			dynT := &Template{
				Name:           t.Name,
				Image:          image,
				Port:           t.Port,
				Pattern:        t.Pattern,
				Pool:           t.Pool,
				Type:           t.Type,
				NoStartupProbe: t.NoStartupProbe,
				Description:    t.Description,
				Metadata:       t.Metadata,
				Args:           t.Args,
				EnvVars:        t.EnvVars,
				Shell:          t.Shell,
				ExtraPorts:     t.ExtraPorts,
			}
			return dynT, nil
		}
	}

	klog.Errorf("Template %s not found", name)
	return nil, fmt.Errorf("Template  %s not found", name)
}

// GetTemplatesForMCPTools return json string, but exclude image field
func GetTemplatesForMCPTools() string {
	type TplForTool struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	var tpls []TplForTool
	for _, env := range Templates {
		if env.Type == "dynamic" {
			continue
		}
		tpls = append(tpls, TplForTool{
			Name:        env.Name,
			Description: env.Description,
		})
	}

	tplsJson, err := json.MarshalIndent(tpls, "", "  ")
	if err != nil {
		klog.Errorf("Failed to marshal Templates for MCP tools: %v", err)
		return ""
	}

	return string(tplsJson)
}
