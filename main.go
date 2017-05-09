package main

import (
	"bytes"
	"encoding/json"
	"github.com/influxdata/influxdb/client/v2"
	"github.com/namsral/flag"
	"github.com/robfig/cron"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	influxAddr        = flag.String("influxdb_address", "http://localhost:8086", "InfluxDB Address")
	influxDBName      = flag.String("influxdb_dbname", "mydb", "InfluxDB Database Name")
	influxMeasurement = flag.String("influxdb_measurement", "docker_stats", "InfluxDB Measurement")
	serverID          = flag.String("server_id", getOutboundIP(), "Server ID")
	serverRole        = flag.String("server_role", "app", "Server Role")
)

type ContainerStat struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MemoryUsage      string `json:"memory_usage"`
	MemoryPercentage string `json:"memory_percentage"`
	CPUPercentage    string `json:"cpu_percentage"`
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()
	idx := strings.LastIndex(localAddr, ":")

	return localAddr[0:idx]
}

func getContainersStats() []ContainerStat {
	out, err := exec.Command("docker", "stats", "--no-stream", "--format", "{\"id\": \"{{.ID}}\", \"name\": \"{{.Name}}\", \"memory_usage\": \"{{.MemUsage}}\", \"memory_percentage\": \"{{.MemPerc}}\", \"cpu_percentage\": \"{{.CPUPerc}}\"},").Output()
	if err != nil {
		log.Printf("%s", err)
	}
	var outFormatted bytes.Buffer
	outFormatted.WriteString("[")
	outFormatted.Write(out[:len(out)-2])
	outFormatted.WriteString("]")

	var stats []ContainerStat

	json.Unmarshal(outFormatted.Bytes(), &stats)

	return stats
}

func sendContainersStats() {

	stats := getContainersStats()

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     *influxAddr,
		Username: "",
		Password: "",
	})
	if err != nil {
		log.Printf("%s", err)
	}
	defer c.Close()

	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  *influxDBName,
		Precision: "s",
	})
	if err != nil {
		log.Printf("%s", err)
	}

	numberRe, _ := regexp.Compile("[+-]?([0-9]*[.])?[0-9]+")

	for i := range stats {
		tags := map[string]string{"server_id": *serverID, "server_role": strings.ToLower(*serverRole), "container": stats[i].Name}
		memUsage, _ := strconv.ParseFloat(numberRe.FindString(stats[i].MemoryUsage), 32)
		memPct, _ := strconv.ParseFloat(numberRe.FindString(stats[i].MemoryPercentage), 32)
		cpuPct, _ := strconv.ParseFloat(numberRe.FindString(stats[i].CPUPercentage), 32)

		fields := map[string]interface{}{
			"memory_usage":      memUsage,
			"memory_percentage": memPct,
			"cpu_percentage":    cpuPct,
		}
		pt, err := client.NewPoint(*influxMeasurement, tags, fields, time.Now())
		if err != nil {
			log.Printf("%s", err)
		}
		bp.AddPoint(pt)
	}
	if err := c.Write(bp); err != nil {
		log.Printf("%s", err)
	}

}

func init() {
	flag.Parse()
}

func main() {
	c := cron.New()
	c.AddFunc("@every 15s", sendContainersStats)
	c.Start()

	defer c.Stop()

	select {}

}
