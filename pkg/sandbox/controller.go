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

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/utils"
	v1 "k8s.io/api/apps/v1"
	v1core "k8s.io/api/core/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metrics "k8s.io/metrics/pkg/client/clientset/versioned"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	rsclient "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset"
	podclient "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
)

type Controller struct {
	kclient       kubernetes.Interface
	MetricsClient *metrics.Clientset
	kcfg          *rest.Config
	rootCtx       context.Context
	pl            *PoolManager
}

func NewController(ctx context.Context, cfg *rest.Config, pl *PoolManager) *Controller {
	c := kubeclient.Get(ctx)
	sh := &Controller{
		rootCtx: ctx,
		kclient: c,
		kcfg:    cfg,
		pl:      pl,
	}
	return sh
}

func (s *Controller) GetRSByID(id string) (*v1.ReplicaSet, error) {
	selector := labels.Set{IDLabel: id, PoolLabel: "false"}.AsSelector()
	rss, err := rsclient.Get(s.rootCtx).Lister().List(selector)
	if err != nil {
		klog.Errorf("Failed to list rs, id %s error %v", id, err)
		return nil, err
	}
	if len(rss) == 0 {
		klog.Warningf("No rs found with id %s", id)
		return nil, fmt.Errorf("no rs found with id %s", id)
	}
	return rss[0], nil
}

func (s *Controller) GetByID(id string) (*Sandbox, error) {
	rs, err := s.GetRSByID(id)
	if err != nil {
		return nil, err
	}
	return s.GetSandbox(rs)
}

func (s *Controller) Get(name string) (*Sandbox, error) {
	selector, _ := labels.Parse(fmt.Sprintf("sandbox=%s", name))
	rss, err := rsclient.Get(s.rootCtx).Lister().ReplicaSets(config.Cfg.SandboxNamespace).List(selector)
	if err != nil {
		return nil, err
	}

	if len(rss) == 0 {
		return nil, fmt.Errorf("no Sandbox found with name %s", name)
	}

	return s.GetSandbox(rss[0])
}

func (s *Controller) GetSandbox(rs *v1.ReplicaSet) (*Sandbox, error) {
	raw := rs.Annotations["sandbox-data"]
	sb := &Sandbox{}
	json.Unmarshal([]byte(raw), sb)
	sb.ReplicaSet = rs.DeepCopy()

	// Set the status of the sandbox
	replicas := *rs.Spec.Replicas
	if replicas == rs.Status.ReadyReplicas {
		sb.Status = Running
	} else {
		sb.Status = Creating
	}

	return sb, nil
}

func (s *Controller) ListPoolByNames(names []string) ([]*Sandbox, error) {
	selectorString := fmt.Sprintf("%s=true,%s in (%s)", PoolLabel, TPLLabel, strings.Join(names, ","))
	selector, _ := labels.Parse(selectorString)

	return s.DoList(selector)
}

func (s *Controller) ListAllPool() ([]*Sandbox, error) {
	selector := labels.Set{
		"owner":   "agent-sandbox",
		PoolLabel: "true",
	}.AsSelector()

	return s.DoList(selector)
}

func (s *Controller) ListPoolSandbox(name string) ([]*Sandbox, error) {
	selector := labels.Set{
		"owner":   "agent-sandbox",
		PoolLabel: "true",
		TPLLabel:  name,
	}.AsSelector()

	return s.DoList(selector)
}

func (s *Controller) ListAll() ([]*Sandbox, error) {
	selector := labels.Set{
		"owner":   "agent-sandbox",
		PoolLabel: "false",
	}.AsSelector()

	return s.DoList(selector)
}

func (s *Controller) List(user string) ([]*Sandbox, error) {
	selector, _ := labels.Parse(fmt.Sprintf("%s=%s", UserLabel, user))
	return s.DoList(selector)
}

func (s *Controller) DoList(selector labels.Selector) ([]*Sandbox, error) {
	rss, err := rsclient.Get(s.rootCtx).Lister().List(selector)
	if err != nil {
		klog.Errorf("failed to list sandboxes: %v", err)
		return nil, err
	}
	var sandboxes = []*Sandbox{}
	for _, rs := range rss {
		raw := rs.Annotations["sandbox-data"]
		sb := &Sandbox{}
		json.Unmarshal([]byte(raw), sb)
		sb.ReplicaSet = rs.DeepCopy()
		// Set the status of the sandbox
		replicas := *rs.Spec.Replicas
		if replicas == rs.Status.ReadyReplicas {
			sb.Status = Running
		} else {
			sb.Status = Creating
		}

		sandboxes = append(sandboxes, sb)
	}
	//sort by CreatedAt desc
	sort.Slice(sandboxes, func(i, j int) bool {
		return sandboxes[i].CreatedAt.After(sandboxes[j].CreatedAt)
	})
	return sandboxes, nil
}

func IsAcquireError(err error) bool {
	return err != nil
}

var CreateRetry = wait.Backoff{
	Steps:    3,
	Duration: 50 * time.Millisecond,
	Factor:   1.0,
	Jitter:   1.0,
}

func (s *Controller) Create(sb *Sandbox) (*Sandbox, error) {
	// retry to AcquirePoolReplicaSet if error is conflict,
	//because multiple sandboxes may try to acquire the same pool replicaset
	acquired := &v1.ReplicaSet{}
	fromPool := false
	err := retry.OnError(CreateRetry, IsAcquireError, func() error {
		var err error
		acquired, fromPool, err = s.pl.AcquirePoolReplicaSet(sb)
		return err
	})

	if err != nil {
		klog.Errorf("failed to create sandbox, error=%v, sandbox=%v", err, sb)
		return nil, fmt.Errorf("failed to create sandbox, error=%v, sandbox=%v", err, sb)
	}

	sb.ReplicaSet = acquired

	// Wait for ReplicaSet to be ready
	if fromPool && sb.TemplateObj.Pool.StartupCmd != "" {
		if perr := s.StartupPoolReplicaSet(sb, false); perr != nil {
			klog.Errorf("timeout waiting for sandbox from pool to be ready: %v, instance not ready yet, please get it leater or check pod status", perr)
			return sb, perr
		}
	} else {
		if perr := s.WaitForReplicaSetReady(sb); perr != nil {
			klog.Errorf("timeout waiting for sandbox to be ready: %v, instance not ready yet, please get it leater or check pod status", perr)
			return sb, perr
		}
	}

	sb.Status = Running
	return sb, nil
}

func (s *Controller) GetInstances(name string) []*v1core.Pod {
	selector, _ := labels.Parse(fmt.Sprintf("sandbox=%s", name))
	pods, err := podclient.Get(s.rootCtx).Lister().Pods(config.Cfg.SandboxNamespace).List(selector)
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return []*v1core.Pod{}
	}
	return pods
}

func (s *Controller) DeleteByID(id string) error {
	rs, err := s.GetRSByID(id)
	if err != nil {
		return err
	}
	return s.Delete(rs.Name)
}

func (s *Controller) Delete(name string) error {
	selector, _ := labels.Parse(fmt.Sprintf("sandbox=%s", name))
	return s.DoDelete(selector)
}

func (s *Controller) DeleteByTemplateName(name string) error {
	selector := labels.Set{
		PoolLabel: "true",
		TPLLabel:  name,
	}.AsSelector()
	return s.DoDelete(selector)
}

func (s *Controller) DoDelete(selector labels.Selector) error {
	// delete rs by selector, since rs name may be different when acquire from pool
	rss, err := rsclient.Get(s.rootCtx).Lister().ReplicaSets(config.Cfg.SandboxNamespace).List(selector)
	if err != nil {
		return err
	}
	for _, rs := range rss {
		err = s.kclient.AppsV1().ReplicaSets(config.Cfg.SandboxNamespace).Delete(context.TODO(), rs.Name, v1meta.DeleteOptions{})
		if err != nil {
			klog.Errorf("failed to delete replicaset %s: %v", rs.Name, err)
			return err
		}
		return err
	}

	return nil
}

type SandboxLogsResult struct {
	Sandbox   string    `json:"sandbox"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Logs      string    `json:"logs"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type SandboxEventInvolvedObject struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	APIVersion string `json:"apiVersion"`
	FieldPath  string `json:"fieldPath"`
}

type SandboxEventItem struct {
	Name           string                     `json:"name"`
	Reason         string                     `json:"reason"`
	Type           string                     `json:"type"`
	Message        string                     `json:"message"`
	Count          int32                      `json:"count"`
	EventTime      time.Time                  `json:"eventTime"`
	FirstTimestamp time.Time                  `json:"firstTimestamp"`
	LastTimestamp  time.Time                  `json:"lastTimestamp"`
	InvolvedObject SandboxEventInvolvedObject `json:"involvedObject"`
}

type SandboxEventsResult struct {
	Items     []SandboxEventItem `json:"items"`
	FetchedAt time.Time          `json:"fetchedAt"`
}

type SandboxTerminalResult struct {
	Sandbox     string    `json:"sandbox"`
	Pod         string    `json:"pod"`
	Container   string    `json:"container"`
	Command     string    `json:"command"`
	Output      string    `json:"output"`
	ErrorOutput string    `json:"errorOutput"`
	ExecutedAt  time.Time `json:"executedAt"`
}

type SandboxTerminalSession struct {
	Sandbox   string `json:"sandbox"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

type SandboxFileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

type SandboxFilesListResult struct {
	Sandbox   string             `json:"sandbox"`
	Pod       string             `json:"pod"`
	Container string             `json:"container"`
	Path      string             `json:"path"`
	Entries   []SandboxFileEntry `json:"entries"`
	FetchedAt time.Time          `json:"fetchedAt"`
}

type SandboxFileUploadResult struct {
	Sandbox    string    `json:"sandbox"`
	Pod        string    `json:"pod"`
	Container  string    `json:"container"`
	Path       string    `json:"path"`
	FileName   string    `json:"fileName"`
	UploadedAt time.Time `json:"uploadedAt"`
}

type SandboxFileDeleteResult struct {
	Sandbox   string    `json:"sandbox"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Path      string    `json:"path"`
	DeletedAt time.Time `json:"deletedAt"`
}

func normalizeSandboxPath(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func parseSandboxFileEntries(output string) []SandboxFileEntry {
	if strings.TrimSpace(output) == "" {
		return []SandboxFileEntry{}
	}

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	entries := make([]SandboxFileEntry, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}

		size, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			size = 0
		}

		entries = append(entries, SandboxFileEntry{
			Name:  parts[0],
			Path:  parts[1],
			IsDir: parts[2] == "1",
			Size:  size,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries
}

func (s *Controller) ListSandboxFiles(name, filePath string) (*SandboxFilesListResult, error) {
	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return nil, err
	}

	const containerName = "sandbox"
	targetPath := normalizeSandboxPath(filePath)

	cmd := []string{"sh", "-lc", fmt.Sprintf("target=%s; if [ ! -d \"$target\" ]; then echo \"path is not a directory: $target\" >&2; exit 1; fi; for entry in \"$target\"/* \"$target\"/.*; do [ -e \"$entry\" ] || continue; base=$(basename \"$entry\"); [ \"$base\" = \".\" ] || [ \"$base\" = \"..\" ] || { if [ -d \"$entry\" ]; then isDir=1; size=0; else isDir=0; size=$(wc -c < \"$entry\" | tr -d ' '); fi; printf '%%s\t%%s\t%%s\t%%s\\n' \"$base\" \"$entry\" \"$isDir\" \"$size\"; }; done", shellQuote(targetPath))}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = utils.ExecStreamCommand(context.Background(), s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, cmd, nil, &stdout, &stderr)
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("failed to list files in pod %s: %w", selected.Name, err)
	}

	return &SandboxFilesListResult{
		Sandbox:   name,
		Pod:       selected.Name,
		Container: containerName,
		Path:      targetPath,
		Entries:   parseSandboxFileEntries(stdout.String()),
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (s *Controller) UploadSandboxFile(name, targetPath string, reader io.Reader, filename string) (*SandboxFileUploadResult, error) {
	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return nil, err
	}

	const containerName = "sandbox"
	normalizedPath := normalizeSandboxPath(targetPath)
	baseName := path.Base(strings.TrimSpace(filename))
	if baseName == "" || baseName == "." || baseName == "/" {
		return nil, fmt.Errorf("invalid filename")
	}

	cmd := []string{"sh", "-lc", fmt.Sprintf("target=%s; file=%s; if [ ! -d \"$target\" ]; then echo \"path is not a directory: $target\" >&2; exit 1; fi; cat > \"$target/$file\"", shellQuote(normalizedPath), shellQuote(baseName))}

	var stderr bytes.Buffer
	err = utils.ExecStreamCommand(context.Background(), s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, cmd, reader, nil, &stderr)
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("failed to upload file to pod %s: %w", selected.Name, err)
	}

	return &SandboxFileUploadResult{
		Sandbox:    name,
		Pod:        selected.Name,
		Container:  containerName,
		Path:       normalizedPath,
		FileName:   baseName,
		UploadedAt: time.Now().UTC(),
	}, nil
}

func (s *Controller) DownloadSandboxFile(name, filePath string, writer io.Writer) error {
	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return err
	}

	const containerName = "sandbox"
	normalizedPath := normalizeSandboxPath(filePath)
	if normalizedPath == "/" {
		return fmt.Errorf("path is required")
	}

	precheckCmd := []string{"sh", "-lc", fmt.Sprintf("target=%s; if [ ! -f \"$target\" ]; then echo \"file not found: $target\" >&2; exit 1; fi", shellQuote(normalizedPath))}
	_, precheckErrOutput, precheckErr := utils.ExecCommand(s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, precheckCmd)
	if precheckErr != nil {
		if strings.TrimSpace(precheckErrOutput) != "" {
			return fmt.Errorf("%s", strings.TrimSpace(precheckErrOutput))
		}
		return fmt.Errorf("failed to validate file in pod %s: %w", selected.Name, precheckErr)
	}

	streamCmd := []string{"sh", "-lc", fmt.Sprintf("target=%s; cat \"$target\"", shellQuote(normalizedPath))}
	var stderr bytes.Buffer
	err = utils.ExecStreamCommand(context.Background(), s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, streamCmd, nil, writer, &stderr)
	if err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("download stream failed: %s", strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("download stream failed for pod %s: %w", selected.Name, err)
	}

	return nil
}

func (s *Controller) DeleteSandboxFile(name, filePath string) (*SandboxFileDeleteResult, error) {
	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return nil, err
	}

	const containerName = "sandbox"
	normalizedPath := normalizeSandboxPath(filePath)
	if normalizedPath == "/" {
		return nil, fmt.Errorf("path is required")
	}

	cmd := []string{"sh", "-lc", fmt.Sprintf("target=%s; if [ ! -f \"$target\" ]; then echo \"file not found or not a regular file: $target\" >&2; exit 1; fi; rm -f -- \"$target\"", shellQuote(normalizedPath))}
	_, stderr, execErr := utils.ExecCommand(s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, cmd)
	if execErr != nil {
		if strings.TrimSpace(stderr) != "" {
			return nil, fmt.Errorf("%s", strings.TrimSpace(stderr))
		}
		return nil, fmt.Errorf("failed to delete path in pod %s: %w", selected.Name, execErr)
	}

	return &SandboxFileDeleteResult{
		Sandbox:   name,
		Pod:       selected.Name,
		Container: containerName,
		Path:      normalizedPath,
		DeletedAt: time.Now().UTC(),
	}, nil
}

func (s *Controller) selectSandboxPod(name string) (*v1core.Pod, error) {
	pods := s.GetInstances(name)
	if len(pods) == 0 {
		return nil, fmt.Errorf("sandbox %s has no pods", name)
	}

	selected := pods[0]
	for _, pod := range pods {
		if pod.Status.Phase == v1core.PodRunning {
			selected = pod
			break
		}
	}

	return selected, nil
}

func (s *Controller) StreamSandboxTerminal(ctx context.Context, name string, command []string, stdin io.Reader, stdout io.Writer, resizeCh <-chan utils.TerminalSize, onReady func(*SandboxTerminalSession)) error {
	if len(command) == 0 {
		return fmt.Errorf("command is required")
	}

	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return err
	}

	const containerName = "sandbox"
	session := &SandboxTerminalSession{
		Sandbox:   name,
		Pod:       selected.Name,
		Container: containerName,
	}
	if onReady != nil {
		onReady(session)
	}

	if err := utils.ExecInteractiveCommand(ctx, s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, containerName, command, stdin, stdout, nil, utils.NewTerminalSizeQueue(resizeCh)); err != nil {
		return fmt.Errorf("failed to stream terminal in pod %s: %w", selected.Name, err)
	}

	return nil
}

func sandboxEventSortTime(item v1core.Event) time.Time {
	if !item.EventTime.Time.IsZero() {
		return item.EventTime.Time
	}
	if !item.LastTimestamp.Time.IsZero() {
		return item.LastTimestamp.Time
	}
	if !item.FirstTimestamp.Time.IsZero() {
		return item.FirstTimestamp.Time
	}
	if !item.CreationTimestamp.Time.IsZero() {
		return item.CreationTimestamp.Time
	}
	return time.Time{}
}

func (s *Controller) ListReplicaSetEvents(name string, limit int64) (*SandboxEventsResult, error) {
	fieldSelector := "involvedObject.kind=ReplicaSet"
	trimmedName := strings.TrimSpace(name)
	if trimmedName != "" {
		fieldSelector = fmt.Sprintf("%s,involvedObject.name=%s", fieldSelector, trimmedName)
	}

	items, err := s.kclient.CoreV1().Events(config.Cfg.SandboxNamespace).List(context.TODO(), v1meta.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	sort.Slice(items.Items, func(i, j int) bool {
		left := sandboxEventSortTime(items.Items[i])
		right := sandboxEventSortTime(items.Items[j])
		if left.Equal(right) {
			return items.Items[i].Name > items.Items[j].Name
		}
		return left.After(right)
	})

	results := make([]SandboxEventItem, 0, len(items.Items))
	for _, item := range items.Items {
		results = append(results, SandboxEventItem{
			Name:           item.Name,
			Reason:         item.Reason,
			Type:           item.Type,
			Message:        item.Message,
			Count:          item.Count,
			EventTime:      item.EventTime.Time.UTC(),
			FirstTimestamp: item.FirstTimestamp.Time.UTC(),
			LastTimestamp:  item.LastTimestamp.Time.UTC(),
			InvolvedObject: SandboxEventInvolvedObject{
				Kind:       item.InvolvedObject.Kind,
				Name:       item.InvolvedObject.Name,
				Namespace:  item.InvolvedObject.Namespace,
				APIVersion: item.InvolvedObject.APIVersion,
				FieldPath:  item.InvolvedObject.FieldPath,
			},
		})
		if int64(len(results)) >= limit {
			break
		}
	}

	return &SandboxEventsResult{
		Items:     results,
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (s *Controller) GetSandboxLogs(name string, tailLines int64) (*SandboxLogsResult, error) {
	pods := s.GetInstances(name)
	if len(pods) == 0 {
		return nil, fmt.Errorf("sandbox %s has no pods", name)
	}

	selected := pods[0]
	for _, pod := range pods {
		if pod.Status.Phase == v1core.PodRunning {
			selected = pod
			break
		}
	}

	containerName := "sandbox"
	options := &v1core.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	}

	stream, err := s.kclient.CoreV1().Pods(config.Cfg.SandboxNamespace).GetLogs(selected.Name, options).Stream(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to stream logs for pod %s: %w", selected.Name, err)
	}
	defer stream.Close()

	logBytes, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read logs for pod %s: %w", selected.Name, err)
	}

	return &SandboxLogsResult{
		Sandbox:   name,
		Pod:       selected.Name,
		Container: containerName,
		Logs:      string(logBytes),
		FetchedAt: time.Now().UTC(),
	}, nil
}

func (s *Controller) StreamContainerLogs(ctx context.Context, sandboxName, container string) (io.ReadCloser, error) {
	pods := s.GetInstances(sandboxName)
	if len(pods) == 0 {
		return nil, fmt.Errorf("sandbox %s has no pods", sandboxName)
	}

	selected := pods[0]
	for _, pod := range pods {
		if pod.Status.Phase == v1core.PodRunning {
			selected = pod
			break
		}
	}

	options := &v1core.PodLogOptions{
		Container: container,
		Follow:    true,
	}

	return s.kclient.CoreV1().Pods(config.Cfg.SandboxNamespace).GetLogs(selected.Name, options).Stream(ctx)
}

func (s *Controller) ExecuteSandboxTerminal(name string, command []string) (*SandboxTerminalResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return nil, err
	}

	const containerName = "sandbox"
	output, errorOutput, err := s.ExecCommandInPod(selected.Name, command)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command in pod %s: %w", selected.Name, err)
	}

	return &SandboxTerminalResult{
		Sandbox:     name,
		Pod:         selected.Name,
		Container:   containerName,
		Command:     strings.Join(command, " "),
		Output:      output,
		ErrorOutput: errorOutput,
		ExecutedAt:  time.Now().UTC(),
	}, nil
}

type SandboxMetricsItem struct {
	Sandbox     string    `json:"sandbox"`
	Pod         string    `json:"pod"`
	CPUMilli    int64     `json:"cpuMilli"`
	MemoryBytes int64     `json:"memoryBytes"`
	MemoryMB    float64   `json:"memoryMB"`
	SampledAt   time.Time `json:"sampledAt"`
}

func pickSandboxContainerMetrics(podMetrics v1beta1.PodMetrics) (v1beta1.ContainerMetrics, bool) {
	for _, container := range podMetrics.Containers {
		if container.Name == "sandbox" {
			return container, true
		}
	}
	if len(podMetrics.Containers) == 0 {
		return v1beta1.ContainerMetrics{}, false
	}
	return podMetrics.Containers[0], true
}

func (s *Controller) SandboxMetrics(names []string) (data map[string]SandboxMetricsItem, err error) {
	data = make(map[string]SandboxMetricsItem)
	if len(names) == 0 {
		return data, nil
	}

	selectorString := fmt.Sprintf("sandbox in (%s)", strings.Join(names, ","))
	podMetricsList, err := s.MetricsClient.MetricsV1beta1().PodMetricses(config.Cfg.SandboxNamespace).List(context.TODO(), v1meta.ListOptions{LabelSelector: selectorString})
	if err != nil {
		return nil, fmt.Errorf("failed to list pod metrics: %w", err)
	}

	for _, podMetrics := range podMetricsList.Items {
		sandboxName := strings.TrimSpace(podMetrics.Labels["sandbox"])
		if sandboxName == "" {
			continue
		}

		container, ok := pickSandboxContainerMetrics(podMetrics)
		if !ok {
			continue
		}

		cpu := container.Usage[v1core.ResourceCPU]
		memory := container.Usage[v1core.ResourceMemory]
		sampledAt := podMetrics.Timestamp.Time.UTC()
		item := SandboxMetricsItem{
			Sandbox:     sandboxName,
			Pod:         podMetrics.Name,
			CPUMilli:    cpu.MilliValue(),
			MemoryBytes: memory.Value(),
			MemoryMB:    float64(memory.Value()) / (1024.0 * 1024.0),
			SampledAt:   sampledAt,
		}

		existing, exists := data[sandboxName]
		if exists && existing.SampledAt.After(item.SampledAt) {
			continue
		}
		data[sandboxName] = item
	}

	return data, nil
}

func (s *Controller) ExecCommandInPod(name string, cmd []string) (output string, outputErr string, err error) {
	return utils.ExecCommand(s.kclient, s.kcfg, config.Cfg.SandboxNamespace, name, "sandbox", cmd)
}

var candidateShells = []string{
	"sh", "bash", "/bin/sh", "/bin/bash", "/usr/bin/sh", "/usr/bin/bash", "/busybox/sh",
}

func (s *Controller) DetectShell(name string) (string, error) {
	selected, err := s.selectSandboxPod(name)
	if err != nil {
		return "", err
	}

	for _, shell := range candidateShells {
		cmd := []string{shell, "-c", "echo ok"}
		out, _, execErr := utils.ExecCommand(s.kclient, s.kcfg, config.Cfg.SandboxNamespace, selected.Name, "sandbox", cmd)
		if execErr == nil && strings.TrimSpace(out) == "ok" {
			return shell, nil
		}
	}

	return "", fmt.Errorf("no usable shell found in container")
}
