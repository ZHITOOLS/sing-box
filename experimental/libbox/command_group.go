package libbox

import (
	"encoding/binary"
	"io"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/outbound"
	"github.com/sagernet/sing/common/rw"
	"github.com/sagernet/sing/service"
)

type OutboundGroup struct {
	Tag        string
	Type       string
	Selectable bool
	Selected   string
	items      []*OutboundGroupItem
}

func (g *OutboundGroup) GetItems() OutboundGroupItemIterator {
	return newIterator(g.items)
}

type OutboundGroupIterator interface {
	Next() *OutboundGroup
	HasNext() bool
}

type OutboundGroupItem struct {
	Tag          string
	Type         string
	URLTestTime  int64
	URLTestDelay int32
}

type OutboundGroupItemIterator interface {
	Next() *OutboundGroupItem
	HasNext() bool
}

func (c *CommandClient) handleGroupConn(conn net.Conn) {
	defer conn.Close()

	for {
		groups, err := readGroups(conn)
		if err != nil {
			c.handler.Disconnected(err.Error())
			return
		}
		c.handler.WriteGroups(groups)
	}
}

func (s *CommandServer) handleGroupConn(conn net.Conn) error {
	defer conn.Close()
	ctx := connKeepAlive(conn)
	for {
		service := s.service
		if service != nil {
			err := writeGroups(conn, service)
			if err != nil {
				return err
			}
		} else {
			err := binary.Write(conn, binary.BigEndian, uint16(0))
			if err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.urlTestUpdate:
		}
	}
}

func readGroups(reader io.Reader) (OutboundGroupIterator, error) {
	var groupLength uint16
	err := binary.Read(reader, binary.BigEndian, &groupLength)
	if err != nil {
		return nil, err
	}

	groups := make([]*OutboundGroup, 0, groupLength)
	for i := 0; i < int(groupLength); i++ {
		var group OutboundGroup
		group.Tag, err = rw.ReadVString(reader)
		if err != nil {
			return nil, err
		}

		group.Type, err = rw.ReadVString(reader)
		if err != nil {
			return nil, err
		}

		err = binary.Read(reader, binary.BigEndian, &group.Selectable)
		if err != nil {
			return nil, err
		}

		group.Selected, err = rw.ReadVString(reader)
		if err != nil {
			return nil, err
		}

		var itemLength uint16
		err = binary.Read(reader, binary.BigEndian, &itemLength)
		if err != nil {
			return nil, err
		}

		group.items = make([]*OutboundGroupItem, itemLength)
		for j := 0; j < int(itemLength); j++ {
			var item OutboundGroupItem
			item.Tag, err = rw.ReadVString(reader)
			if err != nil {
				return nil, err
			}

			item.Type, err = rw.ReadVString(reader)
			if err != nil {
				return nil, err
			}

			err = binary.Read(reader, binary.BigEndian, &item.URLTestTime)
			if err != nil {
				return nil, err
			}

			err = binary.Read(reader, binary.BigEndian, &item.URLTestDelay)
			if err != nil {
				return nil, err
			}

			group.items[j] = &item
		}
		groups = append(groups, &group)
	}
	return newIterator(groups), nil
}

func writeGroups(writer io.Writer, boxService *BoxService) error {
	historyStorage := service.PtrFromContext[urltest.HistoryStorage](boxService.ctx)

	outbounds := boxService.instance.Router().Outbounds()
	var iGroups []adapter.OutboundGroup
	for _, it := range outbounds {
		if group, isGroup := it.(adapter.OutboundGroup); isGroup {
			iGroups = append(iGroups, group)
		}
	}
	var groups []OutboundGroup
	for _, iGroup := range iGroups {
		var group OutboundGroup
		group.Tag = iGroup.Tag()
		group.Type = iGroup.Type()
		_, group.Selectable = iGroup.(*outbound.Selector)
		group.Selected = iGroup.Now()

		for _, itemTag := range iGroup.All() {
			itemOutbound, isLoaded := boxService.instance.Router().Outbound(itemTag)
			if !isLoaded {
				continue
			}

			var item OutboundGroupItem
			item.Tag = itemTag
			item.Type = itemOutbound.Type()
			if history := historyStorage.LoadURLTestHistory(adapter.OutboundTag(itemOutbound)); history != nil {
				item.URLTestTime = history.Time.Unix()
				item.URLTestDelay = int32(history.Delay)
			}
			group.items = append(group.items, &item)
		}
		groups = append(groups, group)
	}

	err := binary.Write(writer, binary.BigEndian, uint16(len(groups)))
	if err != nil {
		return err
	}
	for _, group := range groups {
		err = rw.WriteVString(writer, group.Tag)
		if err != nil {
			return err
		}
		err = rw.WriteVString(writer, group.Type)
		if err != nil {
			return err
		}
		err = binary.Write(writer, binary.BigEndian, group.Selectable)
		if err != nil {
			return err
		}
		err = rw.WriteVString(writer, group.Selected)
		if err != nil {
			return err
		}
		err = binary.Write(writer, binary.BigEndian, uint16(len(group.items)))
		if err != nil {
			return err
		}
		for _, item := range group.items {
			err = rw.WriteVString(writer, item.Tag)
			if err != nil {
				return err
			}
			err = rw.WriteVString(writer, item.Type)
			if err != nil {
				return err
			}
			err = binary.Write(writer, binary.BigEndian, item.URLTestTime)
			if err != nil {
				return err
			}
			err = binary.Write(writer, binary.BigEndian, item.URLTestDelay)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
