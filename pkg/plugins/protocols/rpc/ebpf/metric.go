package ebpf

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/chrismoos/hpack"
	"k8s.io/klog"
	"net"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/erda-project/ebpf-agent/metric"
)

type MapPackage struct {
	//HTTP, RPC, MySQL etc.
	Phase    uint32
	DstIP    string
	DstPort  uint16
	SrcIP    string
	SrcPort  uint16
	Seq      uint16
	Duration uint32
	Pid      uint32
	PathLen  int
	Path     string
	Status   string
}

type Metric struct {
	Phase       uint32
	DstIP       string
	DstPort     uint16
	SrcIP       string
	SrcPort     uint16
	Seq         uint16
	PodName     string
	NodeName    string
	NameSpace   string
	ServiceName string
	Pid         uint32
	PathLen     int
	Path        string
	Status      string
}

func (m *Metric) CovertMetric() metric.Metric {
	var metric metric.Metric
	metric.Measurement = "traffic"
	metric.AddTags("podname", m.PodName)
	metric.AddTags("nodename", m.NodeName)
	metric.AddTags("namespace", m.NameSpace)
	metric.AddTags("servicename", m.ServiceName)
	metric.AddTags("dstip", m.DstIP)
	metric.AddTags("dstport", strconv.Itoa(int(m.DstPort)))
	metric.AddTags("srcip", m.SrcIP)
	metric.AddTags("srcport", strconv.Itoa(int(m.SrcPort)))
	return metric
}

func (m *Metric) String() string {
	return fmt.Sprintf("phase: %d, dstip: %s, dstport: %d, srcip: %s, srcport: %d, seq: %d",
		m.Phase, m.DstIP, m.DstPort, m.SrcIP, m.SrcPort, m.Seq)
}

func DecodeMapItem(e []byte) *MapPackage {
	m := new(MapPackage)
	m.Phase = uint32(e[0])
	m.DstIP = net.IP(e[4:8]).String()
	m.DstPort = binary.BigEndian.Uint16(e[8:12])
	m.SrcIP = net.IP(e[12:16]).String()
	m.SrcPort = binary.BigEndian.Uint16(e[16:20])
	m.Seq = binary.BigEndian.Uint16(e[20:24])
	m.Duration = binary.BigEndian.Uint32(e[24:28])
	m.Pid = binary.LittleEndian.Uint32(e[28:32])
	m.PathLen = int(e[32])
	var err error
	if m.PathLen > 0 && m.PathLen+33 < len(e) {
		m.Path, err = encodeHeader(e[33 : m.PathLen+33+1])
		if err != nil {
			klog.Errorf("encode path header error: %v", err)
		}
	}
	m.Status, err = encodeHeader(e[83:])
	if err != nil {
		klog.Errorf("encode status header error: %v", err)
	}
	return m
}

func encodeHeader(source []byte) (string, error) {
	encodeString := hex.EncodeToString(source)
	encodePath := strings.TrimRight(encodeString, "00")
	encodedHex := []byte(encodePath)
	encoded := make([]byte, len(encodedHex)/2)
	_, err := hex.Decode(encoded, encodedHex)
	if err != nil {
		return "", err
	}
	decoder := hpack.NewDecoder(2048)
	hf, err := decoder.Decode(encoded)
	if err != nil {
		return "", err
	}
	var value string
	for _, h := range hf {
		value = h.Value
	}
	return value, nil
}

// Htons converts to network byte order short uint16.
func Htons(i uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, i)
	return *(*uint16)(unsafe.Pointer(&b[0]))
}

// Htonl converts to network byte order long uint32.
func Htonl(i uint32) uint32 {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, i)
	return *(*uint32)(unsafe.Pointer(&b[0]))
}

// IP4toDec transforms and IPv4 to decimal
func IP4toDec(IPv4Addr string) uint32 {
	bits := strings.Split(IPv4Addr, ".")

	b0, _ := strconv.Atoi(bits[0])
	b1, _ := strconv.Atoi(bits[1])
	b2, _ := strconv.Atoi(bits[2])
	b3, _ := strconv.Atoi(bits[3])

	var sum uint32

	// left shifting 24,16,8,0 and bitwise OR

	sum += uint32(b0) << 24
	sum += uint32(b1) << 16
	sum += uint32(b2) << 8
	sum += uint32(b3)

	return sum
}

// OpenRawSock 创建一个原始的socket套接字
func OpenRawSock(index int) (int, error) {
	// ETH_P_IP: Internet Protocol version 4 (IPv4)
	// ETH_P_ARP: Address Resolution Protocol (ARP)
	// ETH_P_IPV6: Internet Protocol version 6 (IPv6)
	// ETH_P_RARP: Reverse ARP
	// ETH_P_LOOP: Loopback protocol
	const ETH_P_ALL uint16 = 0x03

	sock, err := syscall.Socket(syscall.AF_PACKET,
		syscall.SOCK_RAW|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, int(Htons(ETH_P_ALL)))
	if err != nil {
		return 0, err
	}
	sll := syscall.SockaddrLinklayer{}
	sll.Protocol = Htons(ETH_P_ALL)
	//设置套接字的网卡序号
	sll.Ifindex = index
	if err := syscall.Bind(sock, &sll); err != nil {
		return 0, err
	}
	return sock, nil
}
