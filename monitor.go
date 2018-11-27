package NetMonitor

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"time"

	"github.com/influxdata/influxdb/client/v2"
)

var Terminate = 0

const (
	MyDB          = "test"
	username      = "admin"
	password      = ""
	MyMeasurement = "cpu_usage"
)

type host struct {
	ip        string
	name      string
	id        int16
	seq       int16
	count     byte
	processed bool
	stime     time.Time
	rtime     time.Time
	timeout   int16
	interval  int
	rrt       float64
}

type hostlist struct {
	queue []string
	hash  map[string]host
	size  int
}

type icmpecho struct {
	Type     int8
	Code     int8
	Checksum int16
	Id       int16
	Seq      int16
}

func (h *hostlist) delete(key string, value host) {
	if _, ok := h.hash[key]; ok {
		var index int
		for i, v := range h.queue {
			if v == key {
				index = i
				break
			}
		}
		h.queue = append(h.queue[:index], h.queue[index:]...)

		delete(h.hash, key)
	}
}

func (h *hostlist) append(key string, value host) {

	if h.hash == nil {
		h.hash = make(map[string]host)
	}

	if _, ok := h.hash[key]; !ok {
		//fmt.Println(key)
		h.hash[key] = value
		h.queue = append(h.queue, key)
	}
}

func connInflux() client.Client {
	cli, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     "http://127.0.0.1:8086",
		Username: username,
		Password: password,
	})
	if err != nil {
		log.Fatal(err)
	}
	return cli
}

func cleanup() {
	Terminate = 1
	fmt.Println("cleanup")
}

func WritesPingLog(cli client.Client, h *host) {
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  MyDB,
		Precision: "s",
	})
	if err != nil {
		log.Fatal(err)
	}

	tags := map[string]string{"host": h.ip, "name": h.name}
	fields := map[string]interface{}{
		"rrt": h.rrt,
	}

	pt, err := client.NewPoint(
		"vpn_icmp",
		tags,
		fields,
		h.stime,
		//time.Now(),
	)
	if err != nil {
		log.Fatal(err)
	}
	bp.AddPoint(pt)

	if err := cli.Write(bp); err != nil {
		log.Fatal(err)
	}
}

func pinger(h *host, cl *client.Client, dumpDb bool) error {
	raddr, err := net.ResolveIPAddr("ip", h.ip)
	rand.Seed(time.Now().Unix())
	time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
	if err != nil {
		fmt.Printf("Fail to resolve %s, %s\n", h.ip, err)
		return err
	}

	conn, err := net.DialIP("ip4:icmp", nil, raddr)

	if err != nil {
		fmt.Printf("Fail to connect to remote host: %s\n", err)
		return err
	}
	defer conn.Close()
	var buffer bytes.Buffer

	recv := make([]byte, 1024)

	for Terminate == 0 {
		h.seq = h.seq + 1
		icmp := pkt(h.id, h.seq)
		binary.Write(&buffer, binary.BigEndian, icmp)

		h.processed = false
		h.count = 0

		h.stime = time.Now()
		if _, err := conn.Write(buffer.Bytes()); err != nil {
			return err
		}

		icmphd := icmpecho{}
		for {
			future := h.stime.Add(time.Duration(h.timeout) * time.Millisecond)
			now := time.Now()
			if future.After(now) {
				conn.SetReadDeadline(future)
				_, err := conn.Read(recv)
				now = time.Now()
				if err != nil {
					s := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d",
						now.Year(), now.Month(), now.Day(),
						now.Hour(), now.Minute(), now.Second())
					fmt.Printf("%s %s %s timeout.\n", s, h.ip, h.name)
					h.rrt = -1
					if dumpDb {
						WritesPingLog(*cl, h)
					}

					break
				} else {
					bs := bytes.NewReader(recv[20:28])
					err = binary.Read(bs, binary.BigEndian, &icmphd)
					if icmphd.Id == h.id && icmphd.Seq == h.seq {
						h.count++
						h.rtime = now
						//fmt.Printf("id: %d, seq: %d\n", icmphd.Id, icmphd.Seq)
						s := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d",
							now.Year(), now.Month(), now.Day(),
							now.Hour(), now.Minute(), now.Second())
						h.rrt = float64(int(h.rtime.Sub(h.stime).Seconds()*1000000)) / 1000
						fmt.Printf("%s %s %s %.2f\n", s, h.ip, h.name, h.rrt)
						if dumpDb {
							WritesPingLog(*cl, h)
						}
						break
					}
				}

			}
		}
		buffer.Reset()
		d := h.stime.Add(time.Duration(h.interval) * time.Second).Sub(time.Now())
		time.Sleep(d)

	}

	return err
}

func pkt(id int16, seq int16) []byte {
	icmp := []byte{
		8,
		0,
		0,
		0,
		byte(id >> 8), // identifier (16 bit). zero allowed.
		byte(id),
		byte(seq >> 8), // sequence number (16 bit). zero allowed.
		byte(seq),
	}

	icmp = append(icmp, bytes.Repeat([]byte{'Q'}, 40)...)
	cs := csum(icmp)
	icmp[2] = byte(cs)
	icmp[3] = byte(cs >> 8)
	return icmp
}

func csum(b []byte) uint16 {
	var s uint32
	for i := 0; i < len(b); i += 2 {
		s += uint32(b[i+1])<<8 | uint32(b[i])
	}
	// add back the carry
	s = s>>16 + s&0xffff
	s = s + s>>16
	return uint16(^s)
}

func main() {
	filePtr := flag.String("file", "ip.txt", "host file name")
	dumpDb := flag.Bool("db", false, "dump ping log to database")
	flag.Parse()

	if filePtr == nil {
		fmt.Println("file paramerter has not been provided.")
		return
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	db := connInflux()

	go func() {
		<-c
		cleanup()
	}()

	f, err := os.Open(*filePtr)
	if err != nil {
		fmt.Printf("Error: %s: %s\n", err, *filePtr)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	rand.Seed(time.Now().UnixNano())
	for scanner.Scan() {
		column := strings.Split(scanner.Text(), "\t")
		h := host{}
		h.ip = column[0]
		h.name = column[1]
		i, err := strconv.Atoi(column[2])
		if err != nil {
			fmt.Println(err)
			return
		}
		h.timeout = int16(i)

		r := rand.Intn(65535)
		h.id = int16(r)
		h.seq = 1
		h.interval = 2

		go pinger(&h, &db, *dumpDb)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	for Terminate == 0 {
		time.Sleep(time.Duration(1) * time.Second)
	}

	return

}
