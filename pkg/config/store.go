package config

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const TemplatesConfigMapKey = "config-templates"

// SandboxBlueprintConfigMapKey holds the sandbox blueprint: the k8s Resource
// Definition (ReplicaSet) Go template used to deploy each sandbox.
const SandboxBlueprintConfigMapKey = "config-sandbox-template"

const RuntimeConfigMapKey = "config-runtime"

func WatchConfigMap() func(configMap *corev1.ConfigMap) {
	var lastTemplatesContent string
	var lastSandboxBlueprintContent string
	var lastRuntimeConfigContent string

	return func(configMap *corev1.ConfigMap) {
		templatesContent := configMap.Data[TemplatesConfigMapKey]
		if templatesContent != "" && templatesContent != lastTemplatesContent {
			klog.Info("watching ConfigMap changed, templates content updated, content=", templatesContent)
			Cfg.ShouldLoadTemplates(templatesContent)
			lastTemplatesContent = templatesContent
		}

		sandboxBlueprintContent := configMap.Data[SandboxBlueprintConfigMapKey]
		if sandboxBlueprintContent != "" && sandboxBlueprintContent != lastSandboxBlueprintContent {
			klog.Info("watching ConfigMap changed, sandbox blueprint content updated, content=", sandboxBlueprintContent)
			SandboxBlueprint = sandboxBlueprintContent
			lastSandboxBlueprintContent = sandboxBlueprintContent
		}

		runtimeConfigContent, ok := configMap.Data[RuntimeConfigMapKey]
		if ok && runtimeConfigContent != "" && runtimeConfigContent != lastRuntimeConfigContent {
			klog.Info("watching ConfigMap changed, runtime config updated, content=", runtimeConfigContent)
			Cfg.ApplyRuntimeConfigContent(runtimeConfigContent)
			lastRuntimeConfigContent = runtimeConfigContent
		}
	}
}
