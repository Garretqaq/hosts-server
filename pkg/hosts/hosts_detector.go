package hosts

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-ping/ping"
	"github.com/miekg/dns"
)

const (
	PingTimeoutSec = 1
	PingCount      = 3
	HTTPTimeout    = 5 * time.Second
	MaxConcurrent  = 10
)

var (
	DiscardList = []string{"1.0.1.1", "1.2.1.1", "127.0.0.1"}
	DNSServers  = []string{
		"1.1.1.1:53",         // Cloudflare
		"8.8.8.8:53",         // Google
		"101.101.101.101:53", // Quad101
		"101.102.103.104:53", // Quad101
	}
	PingCache = make(map[string]float64)
	PingMutex = sync.RWMutex{}
)

// HostResult 存储每个域名的检测结果
type HostResult struct {
	Domain string  `json:"domain"`
	IP     string  `json:"ip"`
	Ping   float64 `json:"ping"`
	Error  string  `json:"error,omitempty"`
}

// HostsDetector hosts检测器
type HostsDetector struct {
	domainFile string
	outputFile string
}

// NewHostsDetector 创建新的hosts检测器
func NewHostsDetector(domainFile, outputFile string) *HostsDetector {
	return &HostsDetector{
		domainFile: domainFile,
		outputFile: outputFile,
	}
}

// readDomainFile 读取域名文件
func (hd *HostsDetector) readDomainFile() ([]string, error) {
	file, err := os.Open(hd.domainFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 支持行内注释，截断到第一个 # 之前
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line != "" {
			domains = append(domains, line)
		}
	}
	return domains, scanner.Err()
}

// pingCached 缓存ping结果
func pingCached(ip string) float64 {
	PingMutex.RLock()
	if result, exists := PingCache[ip]; exists {
		PingMutex.RUnlock()
		return result
	}
	PingMutex.RUnlock()

	// 执行ping测试
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		log.Printf("创建pinger失败 %s: %v", ip, err)
		return float64(PingTimeoutSec * 1000)
	}

	// 设置为非特权模式（适用于大部分系统）
	pinger.SetPrivileged(false)
	pinger.Count = PingCount
	pinger.Timeout = time.Duration(PingTimeoutSec) * time.Second

	var pingTimes []float64
	pinger.OnRecv = func(pkt *ping.Packet) {
		pingTimes = append(pingTimes, float64(pkt.Rtt.Nanoseconds())/1e6)
	}

	err = pinger.Run()
	if err != nil || len(pingTimes) == 0 {
		log.Printf("Ping %s 失败: %v", ip, err)
		PingMutex.Lock()
		PingCache[ip] = float64(PingTimeoutSec * 1000)
		PingMutex.Unlock()
		return float64(PingTimeoutSec * 1000)
	}

	// 排序并取中位数
	sort.Float64s(pingTimes)
	median := pingTimes[len(pingTimes)/2]

	fmt.Printf("Ping %s: %.2f ms\n", ip, median)

	PingMutex.Lock()
	PingCache[ip] = median
	PingMutex.Unlock()

	return median
}

// selectBestIP 从IP列表中选择最佳IP
func selectBestIP(ipList []string) string {
	if len(ipList) == 0 {
		return ""
	}

	type ipPing struct {
		IP   string
		Ping float64
	}

	var results []ipPing
	for _, ip := range ipList {
		pingTime := pingCached(ip)
		results = append(results, ipPing{IP: ip, Ping: pingTime})
	}

	// 按ping时间排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Ping < results[j].Ping
	})

	bestIP := results[0].IP
	fmt.Printf("IP候选: %v, 选择: %s (%.2f ms)\n", ipList, bestIP, results[0].Ping)

	return bestIP
}

// getIPFromWeb 从ipaddress.com获取IP地址
func getIPFromWeb(domain string) ([]string, error) {
	url := fmt.Sprintf("https://sites.ipaddress.com/%s", domain)

	client := &http.Client{Timeout: HTTPTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// 提取IP地址
	text := doc.Text()
	ipRegex := regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	ipList := ipRegex.FindAllString(text, -1)

	return ipList, nil
}

// getIPFromDNS 通过DNS查询获取IP地址
func getIPFromDNS(domain string) ([]string, error) {
	var allIPs []string

	for _, server := range DNSServers {
		c := dns.Client{Timeout: 3 * time.Second}
		m := dns.Msg{}
		m.SetQuestion(dns.Fqdn(domain), dns.TypeA)

		r, _, err := c.Exchange(&m, server)
		if err != nil {
			continue
		}

		for _, ans := range r.Answer {
			if a, ok := ans.(*dns.A); ok {
				allIPs = append(allIPs, a.A.String())
			}
		}

		// 如果已经获得了结果，就不需要继续查询其他DNS服务器
		if len(allIPs) > 0 {
			break
		}
	}

	return allIPs, nil
}

// getBestIP 获取域名的最佳IP地址
func getBestIP(domain string) (string, error) {
	var webIPs, dnsIPs []string

	// 并发获取Web和DNS结果
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		webIPs, _ = getIPFromWeb(domain)
	}()

	go func() {
		defer wg.Done()
		dnsIPs, _ = getIPFromDNS(domain)
	}()

	wg.Wait()

	// 合并并去重IP列表
	ipSet := make(map[string]bool)
	for _, ip := range append(webIPs, dnsIPs...) {
		// 过滤掉不需要的IP
		skip := false
		for _, discardIP := range DiscardList {
			if ip == discardIP {
				skip = true
				break
			}
		}
		if !skip && net.ParseIP(ip) != nil {
			ipSet[ip] = true
		}
	}

	if len(ipSet) == 0 {
		return "", fmt.Errorf("未找到有效IP地址")
	}

	// 转换为切片并排序
	var ipList []string
	for ip := range ipSet {
		ipList = append(ipList, ip)
	}
	sort.Strings(ipList)

	fmt.Printf("%s: %v\n", domain, ipList)

	bestIP := selectBestIP(ipList)
	return bestIP, nil
}

// processHost 处理单个域名
func processHost(domain string, resultChan chan<- HostResult) {
	fmt.Printf("开始处理域名: %s\n", domain)

	ip, err := getBestIP(domain)
	result := HostResult{Domain: domain}

	if err != nil {
		result.Error = err.Error()
		result.IP = "# IP Address Not Found"
		fmt.Printf("%s: IP未找到 - %v\n", domain, err)
	} else {
		result.IP = ip
		result.Ping = pingCached(ip)
		fmt.Printf("%s: 选择IP %s (%.2f ms)\n", domain, ip, result.Ping)
	}

	resultChan <- result
}

// WriteHostsFile 写入hosts文件（公开方法）
func (hd *HostsDetector) WriteHostsFile(results []HostResult) error {
	file, err := os.Create(hd.outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// 写入头部注释
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(writer, "# Hosts\n")
	fmt.Fprintf(writer, "# 更新时间: %s\n", currentTime)
	fmt.Fprintf(writer, "# 项目地址: https://github.com/ineo6/hosts\n")
	fmt.Fprintf(writer, "\n")

	// 写入hosts条目
	for _, result := range results {
		line := fmt.Sprintf("%-30s %s", result.IP, result.Domain)

		// 添加超时标记
		if result.Ping >= float64(PingTimeoutSec*1000) {
			line += "  # Timeout"
		}

		fmt.Fprintf(writer, "%s\n", line)
	}

	return nil
}

// GenerateHostsContent 生成hosts内容字符串（公开方法）
func (hd *HostsDetector) GenerateHostsContent(results []HostResult) string {
	var content strings.Builder

	// 写入头部注释
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	content.WriteString("# Hosts\n")
	content.WriteString(fmt.Sprintf("# 更新时间: %s\n", currentTime))
	content.WriteString("# 项目地址: https://github.com/ineo6/hosts\n")
	content.WriteString("\n")

	// 写入hosts条目
	for _, result := range results {
		line := fmt.Sprintf("%-30s %s", result.IP, result.Domain)

		// 添加超时标记
		if result.Ping >= float64(PingTimeoutSec*1000) {
			line += "  # Timeout"
		}

		content.WriteString(line + "\n")
	}

	return content.String()
}

// DetectHosts 检测hosts并返回结果
func (hd *HostsDetector) DetectHosts() ([]HostResult, error) {
	// 读取域名列表
	domains, err := hd.readDomainFile()
	if err != nil {
		return nil, fmt.Errorf("读取域名文件失败: %v", err)
	}

	if len(domains) == 0 {
		return nil, fmt.Errorf("域名文件为空")
	}

	fmt.Printf("共读取到 %d 个域名\n", len(domains))

	// 创建结果通道和工作池
	resultChan := make(chan HostResult, len(domains))
	semaphore := make(chan struct{}, MaxConcurrent)

	// 启动工作协程
	var wg sync.WaitGroup
	for i, domain := range domains {
		wg.Add(1)
		go func(index int, d string) {
			defer wg.Done()
			semaphore <- struct{}{} // 获取信号量
			fmt.Printf("开始处理 %d/%d: %s\n", index+1, len(domains), d)
			processHost(d, resultChan)
			<-semaphore // 释放信号量
		}(i, domain)
	}

	// 等待所有协程完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果
	var results []HostResult
	for result := range resultChan {
		results = append(results, result)
	}

	// 按原始顺序排序结果
	domainOrder := make(map[string]int)
	for i, domain := range domains {
		domainOrder[domain] = i
	}

	sort.Slice(results, func(i, j int) bool {
		return domainOrder[results[i].Domain] < domainOrder[results[j].Domain]
	})

	return results, nil
}

// DetectAndSave 检测hosts并保存到文件
func (hd *HostsDetector) DetectAndSave() error {
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s - 开始执行脚本\n", currentTime)

	results, err := hd.DetectHosts()
	if err != nil {
		return err
	}

	// 写入hosts文件
	err = hd.WriteHostsFile(results)
	if err != nil {
		return fmt.Errorf("写入hosts文件失败: %v", err)
	}

	// 统计结果
	successCount := 0
	for _, result := range results {
		if result.Error == "" {
			successCount++
		}
	}

	currentTime = time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("%s - 脚本执行完成\n", currentTime)
	fmt.Printf("成功解析: %d/%d 个域名\n", successCount, len(results))
	fmt.Printf("结果已写入 %s 文件\n", hd.outputFile)

	return nil
}

// GetHostsContent 获取hosts内容字符串
func (hd *HostsDetector) GetHostsContent() (string, error) {
	results, err := hd.DetectHosts()
	if err != nil {
		return "", err
	}

	return hd.GenerateHostsContent(results), nil
}
