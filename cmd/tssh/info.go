package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"
	"time"
)

// cmdInfo shows detailed information about an instance
func cmdInfo(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: tssh info <name>")
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, args[0])

	fmt.Printf("📋 实例详情\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  名称:      %s\n", inst.Name)
	fmt.Printf("  ID:        %s\n", inst.ID)
	fmt.Printf("  状态:      %s\n", inst.Status)
	fmt.Printf("  内网 IP:   %s\n", inst.PrivateIP)
	if inst.PublicIP != "" {
		fmt.Printf("  公网 IP:   %s\n", inst.PublicIP)
	}
	if inst.EIP != "" {
		fmt.Printf("  EIP:       %s\n", inst.EIP)
	}
	fmt.Printf("  区域:      %s\n", inst.Region)
	fmt.Printf("  可用区:    %s\n", inst.Zone)

	if len(inst.Tags) > 0 {
		fmt.Printf("  标签:\n")
		for k, v := range inst.Tags {
			fmt.Printf("    %s = %s\n", k, v)
		}
	}

	// Fetch live details from API
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	if err != nil {
		return
	}

	detail, err := client.GetInstanceDetail(inst.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n⚠️  无法获取详细信息: %v\n", err)
		return
	}

	fmt.Printf("\n  规格:      %s\n", detail.InstanceType)
	fmt.Printf("  CPU:       %d 核\n", detail.CPU)
	fmt.Printf("  内存:      %.1f GB\n", float64(detail.Memory)/1024)
	fmt.Printf("  操作系统:  %s\n", detail.OSName)
	fmt.Printf("  创建时间:  %s\n", detail.CreationTime)
	fmt.Printf("  到期时间:  %s\n", detail.ExpiredTime)
	fmt.Printf("  付费类型:  %s\n", detail.ChargeType)
	fmt.Printf("  VPC:       %s\n", detail.VpcID)
	fmt.Printf("  安全组:    %s\n", strings.Join(detail.SecurityGroupIDs, ", "))
}

// cmdDoctor runs self-diagnostics
func cmdDoctor() {
	fmt.Println("🩺 tssh 自检诊断")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	allOK := true

	// 1. Check credentials
	fmt.Print("  凭证配置... ")
	config, err := LoadConfig()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		allOK = false
	} else {
		fmt.Printf("✅ profile=%s region=%s\n", config.ProfileName, config.Region)
	}

	// 2. Test API connectivity
	fmt.Print("  API 连通性... ")
	if config != nil {
		client, err := NewAliyunClient(config)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			allOK = false
		} else {
			start := time.Now()
			_, apiErr := client.FetchAllInstances()
			elapsed := time.Since(start)
			if apiErr != nil {
				fmt.Printf("❌ %v\n", apiErr)
				allOK = false
			} else {
				fmt.Printf("✅ (%dms)\n", elapsed.Milliseconds())
			}
		}
	} else {
		fmt.Println("⏭️  跳过 (无凭证)")
	}

	// 3. Check cache
	fmt.Print("  本地缓存... ")
	cache := getCache()
	if !cache.Exists() {
		fmt.Println("⚠️  不存在 (运行 tssh sync)")
		allOK = false
	} else {
		instances, err := cache.Load()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			allOK = false
		} else {
			age := cache.Age()
			status := "✅"
			if age > 24*time.Hour {
				status = "⚠️"
			}
			fmt.Printf("%s %d 台实例, 更新于 %s 前\n", status, len(instances), formatDuration(age))
		}
	}

	// 4. Version + profiles
	fmt.Printf("  tssh 版本... %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	profiles := ListProfiles()
	if len(profiles) > 0 {
		fmt.Printf("  可用 profiles... %s\n", strings.Join(profiles, ", "))
	}

	if allOK {
		fmt.Println("\n✅ 所有检查通过")
	} else {
		fmt.Println("\n⚠️  部分检查未通过")
	}
}

// formatDuration formats a duration into human-readable text
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1f天", d.Hours()/24)
}

// execLookPath wraps exec.LookPath
func execLookPath(name string) (string, error) {
	return osexec.LookPath(name)
}

// cmdUpdate self-updates from GitHub Releases
func cmdUpdate() {
	fmt.Printf("🔄 检查更新... (当前: v%s)\n", version)

	// Bounded timeout on the GitHub API call — DefaultClient hangs forever.
	apiClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := apiClient.Get("https://api.github.com/repos/wangxiuwen/tssh/releases/latest")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 无法连接 GitHub: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 解析失败: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == version {
		fmt.Println("✅ 已是最新版本")
		return
	}

	fmt.Printf("📦 发现新版本: v%s → v%s\n", version, latestVersion)

	// Find matching asset
	target := fmt.Sprintf("tssh-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, target) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "❌ 没有找到 %s 的预编译包\n", target)
		os.Exit(1)
	}

	fmt.Printf("⬇️  下载 %s...\n", target)

	// 5 min cap — binary is ~10MB but slow mirrors can stretch; anything longer
	// than this is probably a hung connection rather than honest throughput.
	dlClient := &http.Client{Timeout: 5 * time.Minute}
	dlResp, err := dlClient.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 下载失败: %v\n", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 无法获取当前路径: %v\n", err)
		os.Exit(1)
	}

	tmpPath := execPath + ".new"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 无法写入: %v\n", err)
		os.Exit(1)
	}

	_, err = io.Copy(f, dlResp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "❌ 下载中断: %v\n", err)
		os.Exit(1)
	}

	// Replace old binary
	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "❌ 替换失败: %v (try: sudo tssh update)\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ 更新成功! v%s → v%s\n", version, latestVersion)
}
