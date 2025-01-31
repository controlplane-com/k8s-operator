package realtime

import "time"

type Sync interface {
	Close() error
}

var syncs = map[string]Sync{}

func RegisterSync(name string, sync Sync) {
	syncs[name] = sync
}
func GetSync(name string) Sync {
	return syncs[name]
}
func DeregisterSync(name string) error {
	s, ok := syncs[name].(Sync)
	if !ok {
		return nil
	}
	delete(syncs, name)
	return s.Close()
}

type Message[T any] struct {
	Data      T         `json:"data"`
	EventType string    `json:"eventType"`
	Id        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}
