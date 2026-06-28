package stages

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/pkg/openlist"
	"github.com/bililive-go/bililive-go/src/tools"
)

type ExtractCoverStage struct {
	config   pipeline.StageConfig
	commands []string
	logs     string
}

func NewExtractCoverStage(config pipeline.StageConfig) (pipeline.Stage, error) {
	return &ExtractCoverStage{
		config: config,
	}, nil
}

func (s *ExtractCoverStage) Name() string {
	return pipeline.StageNameExtractCover
}

func (s *ExtractCoverStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	if len(input) == 0 {
		s.logs = "没有输入文件"
		return input, nil
	}

	output := append([]pipeline.FileInfo{}, input...)

	for _, file := range input {
		if file.Type != pipeline.FileTypeVideo {
			continue
		}

		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			s.logs += fmt.Sprintf("文件不存在: %s\n", file.Path)
			continue
		}

		ctx.Logger.Infof("提取封面: %s", file.Path)

		coverPath, err := tools.ExtractCover(ctx.Ctx, file.Path)
		if err != nil {
			s.logs += fmt.Sprintf("提取封面失败: %s - %s\n", filepath.Base(file.Path), err.Error())
			ctx.Logger.Warnf("提取封面失败: %s - %s", file.Path, err)
			continue
		}

		output = append(output, pipeline.FileInfo{
			Path:       coverPath,
			Type:       pipeline.FileTypeCover,
			SourcePath: file.Path,
		})

		s.logs += fmt.Sprintf("封面已保存: %s\n", filepath.Base(coverPath))
		ctx.Logger.Infof("封面已保存: %s", coverPath)
	}

	return output, nil
}

func (s *ExtractCoverStage) GetCommands() []string {
	return s.commands
}

func (s *ExtractCoverStage) GetLogs() string {
	return s.logs
}

type openListProvider interface {
	IsRunning() bool
	GetAPIEndpoint() string
	GetAPIToken() string
}

var (
	openListProviderMu  sync.RWMutex
	openListProviderRef openListProvider
)

func SetOpenListProvider(provider openListProvider) {
	openListProviderMu.Lock()
	defer openListProviderMu.Unlock()
	openListProviderRef = provider
}

func getOpenListProvider() openListProvider {
	openListProviderMu.RLock()
	defer openListProviderMu.RUnlock()
	return openListProviderRef
}

type CloudUploadStage struct {
	config             pipeline.StageConfig
	storageName        string
	additionalStorages []string
	pathTemplate       string
	deleteAfter        bool
	fileTypes          []string
	commands           []string
	logs               string
}

func NewCloudUploadStage(config pipeline.StageConfig) (pipeline.Stage, error) {
	return &CloudUploadStage{
		config:             config,
		storageName:        config.GetStringOption(pipeline.OptionStorage, ""),
		additionalStorages: config.GetStringSliceOption(pipeline.OptionAdditionalStorages),
		pathTemplate:       config.GetStringOption(pipeline.OptionPathTemplate, ""),
		deleteAfter:        config.GetBoolOption(pipeline.OptionDeleteAfter, false),
		fileTypes:          config.GetStringSliceOption(pipeline.OptionFileTypes),
	}, nil
}

func (s *CloudUploadStage) Name() string {
	return pipeline.StageNameCloudUpload
}

func (s *CloudUploadStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	if len(input) == 0 {
		s.logs = "没有输入文件"
		return input, nil
	}

	if strings.TrimSpace(s.storageName) == "" {
		s.logs = "未配置存储位置，跳过上传"
		return input, nil
	}

	client, err := s.newOpenListClient()
	if err != nil {
		s.logs = err.Error()
		return input, err
	}

	storageRoots := s.storageRoots()
	if len(storageRoots) == 0 {
		s.logs = "未配置有效存储位置，跳过上传"
		return input, nil
	}

	var output []pipeline.FileInfo
	for _, file := range input {
		if len(s.fileTypes) > 0 && !s.matchFileType(file.Type) {
			output = append(output, file)
			continue
		}

		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			s.logs += fmt.Sprintf("文件不存在: %s\n", file.Path)
			continue
		}

		targetPath := s.renderTargetPath(ctx, file)
		if targetPath == "" {
			s.logs += fmt.Sprintf("无法生成目标路径: %s\n", file.Path)
			output = append(output, file)
			continue
		}

		for _, storageRoot := range storageRoots {
			remotePath := joinRemotePath(storageRoot, targetPath)
			if remotePath == "" {
				err := fmt.Errorf("无法生成上传目标路径: storage=%s target=%s", storageRoot, targetPath)
				s.logs += err.Error() + "\n"
				return output, err
			}

			if err := s.ensureRemoteParentDir(ctx, client, storageRoot, remotePath); err != nil {
				wrapped := fmt.Errorf("创建 OpenList 目录失败 %s: %w", pathpkg.Dir(remotePath), err)
				s.logs += wrapped.Error() + "\n"
				return output, wrapped
			}

			ctx.Logger.Infof("OpenList 上传开始: %s -> %s", file.Path, remotePath)
			s.commands = append(s.commands, fmt.Sprintf("upload %s to %s", file.Path, remotePath))
			if err := client.Upload(ctx.Ctx, file.Path, remotePath, s.progressLogger(ctx, filepath.Base(file.Path), remotePath)); err != nil {
				wrapped := fmt.Errorf("OpenList 上传失败 %s -> %s: %w", filepath.Base(file.Path), remotePath, err)
				s.logs += wrapped.Error() + "\n"
				ctx.Logger.WithError(err).Errorf("OpenList 上传失败: %s -> %s", file.Path, remotePath)
				return output, wrapped
			}

			s.logs += fmt.Sprintf("上传成功: %s -> %s\n", filepath.Base(file.Path), remotePath)
			ctx.Logger.Infof("OpenList 上传完成: %s -> %s", file.Path, remotePath)
		}

		if !s.deleteAfter {
			output = append(output, file)
			continue
		}

		if err := os.Remove(file.Path); err != nil {
			wrapped := fmt.Errorf("上传后删除本地文件失败 %s: %w", filepath.Base(file.Path), err)
			s.logs += wrapped.Error() + "\n"
			return output, wrapped
		}
		s.logs += fmt.Sprintf("上传后删除: %s\n", filepath.Base(file.Path))
	}

	return output, nil
}

func (s *CloudUploadStage) matchFileType(fileType pipeline.FileType) bool {
	for _, ft := range s.fileTypes {
		if strings.EqualFold(ft, string(fileType)) {
			return true
		}
	}
	return false
}

func (s *CloudUploadStage) renderTargetPath(ctx *pipeline.PipelineContext, file pipeline.FileInfo) string {
	if s.pathTemplate == "" {
		return fmt.Sprintf("/录播归档/%s/%s/%s",
			ctx.RecordInfo.Platform,
			ctx.RecordInfo.HostName,
			filepath.Base(file.Path),
		)
	}

	path := s.pathTemplate
	path = strings.ReplaceAll(path, "{{ .Platform }}", ctx.RecordInfo.Platform)
	path = strings.ReplaceAll(path, "{{.Platform}}", ctx.RecordInfo.Platform)
	path = strings.ReplaceAll(path, "{{ .HostName }}", ctx.RecordInfo.HostName)
	path = strings.ReplaceAll(path, "{{.HostName}}", ctx.RecordInfo.HostName)
	path = strings.ReplaceAll(path, "{{ .RoomName }}", ctx.RecordInfo.RoomName)
	path = strings.ReplaceAll(path, "{{.RoomName}}", ctx.RecordInfo.RoomName)
	path = strings.ReplaceAll(path, "{{ .FileName }}", filepath.Base(file.Path))
	path = strings.ReplaceAll(path, "{{.FileName}}", filepath.Base(file.Path))

	ext := filepath.Ext(file.Path)
	if len(ext) > 0 && ext[0] == '.' {
		ext = ext[1:]
	}
	path = strings.ReplaceAll(path, "{{ .Ext }}", ext)
	path = strings.ReplaceAll(path, "{{.Ext}}", ext)

	return path
}

func (s *CloudUploadStage) storageRoots() []string {
	raw := append([]string{s.storageName}, s.additionalStorages...)
	seen := make(map[string]struct{}, len(raw))
	roots := make([]string, 0, len(raw))
	for _, item := range raw {
		root := normalizeRemotePath(item)
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

func (s *CloudUploadStage) newOpenListClient() (*openlist.Client, error) {
	if provider := getOpenListProvider(); provider != nil && provider.IsRunning() {
		endpoint := strings.TrimSpace(provider.GetAPIEndpoint())
		if endpoint != "" {
			return openlist.NewClient(endpoint, provider.GetAPIToken()), nil
		}
	}

	cfg := configs.GetCurrentConfig()
	if cfg != nil {
		if endpoint := strings.TrimSpace(cfg.OpenList.ExternalURL); endpoint != "" {
			return openlist.NewClient(endpoint, cfg.OpenList.ExternalToken), nil
		}
	}

	return nil, fmt.Errorf("OpenList 未启动或未配置，无法执行云上传")
}

func (s *CloudUploadStage) ensureRemoteParentDir(ctx *pipeline.PipelineContext, client *openlist.Client, storageRoot, remoteFilePath string) error {
	parent := normalizeRemotePath(pathpkg.Dir(remoteFilePath))
	root := normalizeRemotePath(storageRoot)
	if parent == "" || parent == "/" || parent == root {
		return nil
	}

	relative := strings.Trim(strings.TrimPrefix(parent, root), "/")
	if relative == "" || relative == parent {
		return client.Mkdir(ctx.Ctx, parent)
	}

	current := root
	for _, segment := range strings.Split(relative, "/") {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		current = joinRemotePath(current, segment)
		if err := client.Mkdir(ctx.Ctx, current); err != nil {
			return err
		}
	}
	return nil
}

func (s *CloudUploadStage) progressLogger(ctx *pipeline.PipelineContext, fileName, remotePath string) func(openlist.UploadProgress) {
	lastLog := time.Time{}
	return func(progress openlist.UploadProgress) {
		now := time.Now()
		if progress.Percentage < 100 && !lastLog.IsZero() && now.Sub(lastLog) < 5*time.Second {
			return
		}
		lastLog = now
		ctx.Logger.Infof(
			"OpenList 上传进度: %s -> %s %.1f%% %s/s",
			fileName,
			remotePath,
			progress.Percentage,
			formatBytes(progress.SpeedBytesPerSec),
		)
	}
}

func normalizeRemotePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	if value == "" {
		return ""
	}
	cleaned := pathpkg.Clean("/" + strings.Trim(value, "/"))
	if cleaned == "/" || cleaned == "." {
		return ""
	}
	return cleaned
}

func joinRemotePath(root, child string) string {
	root = normalizeRemotePath(root)
	child = normalizeRemotePath(child)
	if root == "" {
		return child
	}
	if child == "" {
		return root
	}
	return normalizeRemotePath(root + "/" + strings.TrimPrefix(child, "/"))
}

func formatBytes(bytesPerSecond int64) string {
	const unit = 1024
	if bytesPerSecond < unit {
		return fmt.Sprintf("%d B", bytesPerSecond)
	}
	value := float64(bytesPerSecond)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

func (s *CloudUploadStage) GetCommands() []string {
	return s.commands
}

func (s *CloudUploadStage) GetLogs() string {
	return s.logs
}
