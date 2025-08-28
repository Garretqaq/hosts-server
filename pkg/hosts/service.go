package hosts

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// HostsService hosts服务结构体
type HostsService struct {
	detector *HostsDetector
}

// NewHostsService 创建新的hosts服务
func NewHostsService(domainFile, outputFile string) *HostsService {
	return &HostsService{
		detector: NewHostsDetector(domainFile, outputFile),
	}
}

// Response 统一响应结构体
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// HostsResponse hosts响应结构体
type HostsResponse struct {
	Content   string       `json:"content"`
	Results   []HostResult `json:"results"`
	UpdatedAt string       `json:"updated_at"`
	Total     int          `json:"total"`
	Success   int          `json:"success"`
}

// getHosts 获取hosts内容
func (hs *HostsService) getHosts(c *gin.Context) {
	// 检测hosts
	results, err := hs.detector.DetectHosts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "检测hosts失败: " + err.Error(),
		})
		return
	}

	// 生成hosts内容
	content := hs.detector.GenerateHostsContent(results)

	// 统计成功数量
	successCount := 0
	for _, result := range results {
		if result.Error == "" {
			successCount++
		}
	}

	// 保存到文件
	if err := hs.detector.WriteHostsFile(results); err != nil {
		// 即使保存失败也返回内容，只是记录错误
		c.Header("X-Save-Error", err.Error())
	}

	response := HostsResponse{
		Content:   content,
		Results:   results,
		UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
		Total:     len(results),
		Success:   successCount,
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取hosts成功",
		Data:    response,
	})
}

// getHostsRaw 获取原始hosts文件内容
func (hs *HostsService) getHostsRaw(c *gin.Context) {
	// 检测hosts
	content, err := hs.detector.GetHostsContent()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "检测hosts失败: " + err.Error(),
		})
		return
	}

	// 设置响应头为纯文本
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=hosts")

	c.String(http.StatusOK, content)
}

// getHostsFile 获取已保存的hosts文件内容
func (hs *HostsService) getHostsFile(c *gin.Context) {
	// 直接返回文件内容
	filePath := filepath.Clean(hs.detector.outputFile)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=hosts")
	c.File(filePath)
}

// getStatus 获取服务状态
func (hs *HostsService) getStatus(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "服务正常运行",
		Data: gin.H{
			"service":     "Hosts 检测服务",
			"version":     "1.0.0",
			"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
			"domain_file": hs.detector.domainFile,
			"output_file": hs.detector.outputFile,
		},
	})
}

// setupRoutes 设置路由
func (hs *HostsService) setupRoutes() *gin.Engine {
	// 设置gin模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// 中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 添加CORS中间件
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// API路由组
	api := router.Group("/api/v1")
	{
		api.GET("/status", hs.getStatus)        // 服务状态
		api.GET("/hosts", hs.getHosts)          // 获取hosts（JSON格式）
		api.GET("/hosts/raw", hs.getHostsRaw)   // 获取hosts原始内容
		api.GET("/hosts/file", hs.getHostsFile) // 获取已保存的hosts文件
	}

	// 根路径重定向到状态页面
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/api/v1/status")
	})

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		})
	})

	return router
}

// Start 启动HTTP服务
func (hs *HostsService) Start(port string) error {
	router := hs.setupRoutes()

	if port == "" {
		port = ":8080"
	}
	if port[0] != ':' {
		port = ":" + port
	}

	return router.Run(port)
}
