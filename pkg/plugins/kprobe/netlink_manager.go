package kprobe

import (
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"net"
)

type LinkEventType string

var (
	LinkAdd    LinkEventType = "add"
	LinkDelete LinkEventType = "delete"
)

type NeighLink struct {
	Neigh netlink.Neigh
	Link  netlink.Link
}

type NeighLinkEvent struct {
	Type LinkEventType
	NeighLink
}

func getAllVethes() ([]NeighLink, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	targetLinks := make([]netlink.Link, 0)
	for _, link := range links {
		if link.Type() == "bridge" {
			targetLinks = append(targetLinks, link)
		}
		if link.Type() == "veth" {
			targetLinks = append(targetLinks, link)
		}
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	ans := make([]NeighLink, 0)
	for _, l := range targetLinks {
		neighs, err := netlink.NeighList(l.Attrs().Index, unix.AF_INET)
		if err != nil {
			return nil, err
		}
		// veth bind neigh ip, like vethvethfb8d1967, vethXXX and so on
		if len(neighs) == 1 && l.Type() == "veth" {
			ans = append(ans, NeighLink{
				Neigh: neighs[0],
				Link:  l,
			})
			continue
		}
		// bridge bind veth neigh ips, like calie9c9ef7ca49, caliXXX binded to docker0
		for _, neigh := range neighs {
			for _, iface := range ifs {
				link, err := netlink.LinkByName(iface.Name)
				if err != nil {
					continue
				}
				if link.Type() == "veth" {
					neighBr, errBr := netlink.NeighList(link.Attrs().Index, int(unix.AF_BRIDGE))
					if errBr != nil {
						continue
					}
					for _, neighB := range neighBr {
						if neighB.HardwareAddr.String() == neigh.HardwareAddr.String() {
							ans = append(ans, NeighLink{
								Neigh: neigh,
								Link:  link,
							})
							break
						}
					}
				}
			}
		}
	}
	return ans, nil
}

func (p *provider) getVethesDiff() (added []NeighLink, removed []NeighLink, err error) {
	neighs, err := getAllVethes()
	if err != nil {
		return nil, nil, err
	}
	allNeighSet := make(map[int]NeighLink)
	p.RLock()
	defer p.RUnlock()
	for _, neigh := range p.netLinks {
		allNeighSet[neigh.Link.Attrs().Index] = neigh
	}

	// added neighs
	for _, neigh := range neighs {
		delete(allNeighSet, neigh.Link.Attrs().Index)
		if _, ok := p.netLinks[neigh.Link.Attrs().Index]; !ok {
			added = append(added, neigh)
		}
	}

	// removed neighs
	for _, neigh := range allNeighSet {
		removed = append(removed, neigh)
	}
	return
}

func (p *provider) refreshVethes() error {
	added, removed, err := p.getVethesDiff()
	if err != nil {
		return err
	}
	p.Lock()
	for _, neigh := range added {
		p.netLinks[neigh.Link.Attrs().Index] = neigh
	}
	for _, neigh := range removed {
		delete(p.netLinks, neigh.Link.Attrs().Index)
	}
	p.Unlock()

	for _, neigh := range added {
		for _, ch := range p.netLinkListeners {
			ch <- NeighLinkEvent{
				Type:      LinkAdd,
				NeighLink: neigh,
			}
		}
	}
	for _, neigh := range removed {
		for _, ch := range p.netLinkListeners {
			ch <- NeighLinkEvent{
				Type:      LinkDelete,
				NeighLink: neigh,
			}
		}
	}
	return nil
}
