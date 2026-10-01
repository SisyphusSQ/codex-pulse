package utils

import (
	"net"
)

func GetIP() (ipv4 string, err error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return
	}

	for _, iface := range interfaces {
		// 忽略没有地址的接口
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP

			// 检查地址类型并提取 IP 字段
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// 忽略环回地址和 IPv6 地址
			if ip == nil || ip.IsLoopback() {
				continue
			}

			// 使用 IPv4 地址
			if ip.To4() != nil {
				ipv4 = ip.String()
			}
		}
	}
	return
}
