package overlayfs

import (
	"strings"
	"sync"
)

// memStore keeps whiteout and opaque marks in memory. The top layer lives in
// memory and is discarded with the process, so its masking state needs no
// durability, and the sqlite store upstream ships would only add a database
// inside memfs.
type memStore struct {
	mu        sync.Mutex
	whiteouts map[string]bool
	opaques   map[string]bool
}

func newMemStore() *memStore {
	return &memStore{whiteouts: map[string]bool{}, opaques: map[string]bool{}}
}

func (s *memStore) Load() (whiteouts, opaques []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for n := range s.whiteouts {
		whiteouts = append(whiteouts, n)
	}
	for n := range s.opaques {
		opaques = append(opaques, n)
	}

	return whiteouts, opaques, nil
}

func (s *memStore) SetWhiteout(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := name + "/"
	for n := range s.whiteouts {
		if strings.HasPrefix(n, prefix) {
			delete(s.whiteouts, n)
		}
	}
	for n := range s.opaques {
		if n == name || strings.HasPrefix(n, prefix) {
			delete(s.opaques, n)
		}
	}

	s.whiteouts[name] = true
	return nil
}

func (s *memStore) ClearWhiteout(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.whiteouts, name)
	return nil
}

func (s *memStore) SetOpaque(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.opaques[dir] = true
	return nil
}

func (s *memStore) ClearOpaque(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.opaques, dir)
	return nil
}

func (*memStore) Close() error { return nil }
