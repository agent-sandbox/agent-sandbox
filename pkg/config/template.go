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
	"fmt"
	"regexp"
	"strings"

	"k8s.io/klog/v2"
)

type Resources struct {
	CPU         string `json:"cpu"`
	Memory      string `json:"memory"`
	CPULimit    string `json:"cpuLimit"`
	MemoryLimit string `json:"memoryLimit"`
}

type TemplatePool struct {
	Size       int    `json:"size" required:"false"`
	ReadySize  int    `json:"readySize,omitempty" required:"false"`
	ProbePort  int    `json:"probePort" required:"false"`
	WarmupCmd  string `json:"warmupCmd" required:"false"`
	StartupCmd string `json:"startupCmd" required:"false"`
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
	Resources      Resources         `json:"resources"  required:"false"`
	Pool           TemplatePool      `json:"pool" required:"false"`
	Description    string            `json:"description" required:"false"`
}

func GetTemplateByName(name string) (*Template, error) {
	for _, t := range Templates {
		if t.Name == name {
			return t, nil
		}
	}

	for _, t := range Templates {
		if t.Type != "dynamic" {
			continue
		}
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
		}
		return dynT, nil
	}

	klog.Errorf("Template %s not found", name)
	return nil, fmt.Errorf("Template  %s not found", name)
}
