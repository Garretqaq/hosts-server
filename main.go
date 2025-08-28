package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"hosts-server/pkg/hosts"
)

// 命令行参数
var (
	serverMode = flag.Bool("server", true, "启动HTTP服务模式")
	port       = flag.String("port", "8585", "HTTP服务端口")
	domainFile = flag.String("domain", "domain.txt", "域名文件路径")
	outputFile = flag.String("output", "hosts", "输出hosts文件路径")
	help       = flag.Bool("help", false, "显示帮助信息")
)

func main() {
	flag.Parse()

	// 显示帮助信息
	if *help {
		printHelp()
		return
	}

	// 根据参数决定运行模式
	if *serverMode {
		startServer()
	} else {
		runSingleDetection()
	}
}

// printHelp 打印帮助信息
func printHelp() {
	fmt.Printf("Hosts 检测工具\n\n")
	fmt.Printf("使用方法:\n")
	fmt.Printf("  %s [选项]\n\n", os.Args[0])
	fmt.Printf("选项:\n")
	fmt.Printf("  -server         启动HTTP服务模式（默认为单次检测模式）\n")
	fmt.Printf("  -port string    HTTP服务端口 (默认 \"8585\")\n")
	fmt.Printf("  -domain string  域名文件路径 (默认 \"domain.txt\")\n")
	fmt.Printf("  -output string  输出hosts文件路径 (默认 \"hosts\")\n")
	fmt.Printf("  -help           显示此帮助信息\n\n")
	fmt.Printf("示例:\n")
	fmt.Printf("  %s                          # 单次检测并保存hosts文件\n", os.Args[0])
	fmt.Printf("  %s -server                  # 启动HTTP服务 (端口8585)\n", os.Args[0])
	fmt.Printf("  %s -server -port 9000       # 启动HTTP服务 (端口9000)\n", os.Args[0])
	fmt.Printf("  %s -domain mydomain -output myhosts  # 使用自定义文件路径\n", os.Args[0])
}

// runSingleDetection 运行单次检测
func runSingleDetection() {
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s - 开始执行脚本\n", currentTime)

	// 使用指定配置运行检测
	detector := hosts.NewHostsDetector(*domainFile, *outputFile)
	if err := detector.DetectAndSave(); err != nil {
		log.Fatalf("检测失败: %v", err)
	}
}

// startServer 启动HTTP服务
func startServer() {
	fmt.Printf("启动Hosts检测服务...\n")
	fmt.Printf("服务地址: http://localhost:%s\n\n", *port)

	// 创建hosts检测器
	detector := hosts.NewHostsDetector(*domainFile, *outputFile)

	// 定时检测：启动时立即检测一次，之后每3小时检测
	go func() {
		fmt.Printf("[定时任务] 启动时进行一次检测并写入文件...\n")
		if err := detector.DetectAndSave(); err != nil {
			log.Printf("[定时任务] 启动检测失败: %v\n", err)
		}

		ticker := time.NewTicker(3 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("[定时任务] 每3小时检测一次并写入文件...\n")
			if err := detector.DetectAndSave(); err != nil {
				log.Printf("[定时任务] 检测失败: %v\n", err)
			}
		}
	}()

	// 状态接口
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		response := map[string]interface{}{
			"code":    200,
			"message": "服务正常运行",
			"data": map[string]interface{}{
				"service":     "Hosts 检测服务",
				"version":     "1.0.0",
				"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
				"domain_file": *domainFile,
				"output_file": *outputFile,
			},
		}

		json.NewEncoder(w).Encode(response)
	})

	// 健康检查
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		response := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		}

		json.NewEncoder(w).Encode(response)
	})


	// 获取当前hosts文件内容（原始文本）
	http.HandleFunc("/hosts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Disposition", "attachment; filename=hosts")

		content, err := os.ReadFile(*outputFile)
		if err != nil {
			http.Error(w, "读取hosts文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write(content)
	})

	// 实时检测并获取hosts内容（JSON格式）
	http.HandleFunc("/hosts/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		fmt.Printf("开始实时检测hosts（JSON格式）...\n")

		results, err := detector.DetectHosts()
		if err != nil {
			response := map[string]interface{}{
				"code":    500,
				"message": "检测hosts失败: " + err.Error(),
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(response)
			return
		}

		// 生成hosts内容
		content := detector.GenerateHostsContent(results)

		// 统计成功数量
		successCount := 0
		for _, result := range results {
			if result.Error == "" {
				successCount++
			}
		}

		// 保存到文件
		if err := detector.WriteHostsFile(results); err != nil {
			w.Header().Set("X-Save-Error", err.Error())
		}

		response := map[string]interface{}{
			"code":    200,
			"message": "获取hosts成功",
			"data": map[string]interface{}{
				"content":    content,
				"results":    results,
				"updated_at": time.Now().Format("2006-01-02 15:04:05"),
				"total":      len(results),
				"success":    successCount,
			},
		}

		fmt.Printf("实时检测完成，成功: %d/%d\n", successCount, len(results))
		json.NewEncoder(w).Encode(response)
	})

	// 根路径重定向
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/status", http.StatusMovedPermanently)
			return
		}
		http.NotFound(w, r)
	})

	fmt.Printf("可用的API接口:\n")
	fmt.Printf("  GET  /status      - 服务状态\n")
	fmt.Printf("  GET  /hosts       - 获取当前hosts文件（原始文本）\n")
	fmt.Printf("  GET  /hosts/json  - 实时检测hosts（JSON格式）\n")
	fmt.Printf("  GET  /health      - 健康检查\n")
	fmt.Printf("定时任务: 启动时立即检测，之后每3小时检测一次\n")
	fmt.Printf("\n服务启动中...\n")

	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
