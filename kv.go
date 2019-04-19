package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/abronan/valkeyrie"
	"github.com/abronan/valkeyrie/store"
	"github.com/abronan/valkeyrie/store/boltdb"
	"github.com/abronan/valkeyrie/store/consul"
	etcdv3 "github.com/abronan/valkeyrie/store/etcd/v3"
	"github.com/abronan/valkeyrie/store/zookeeper"
)

const (
	storeKey = "traefik/acme/account/object"
)

func getStoredDataFromGzip(value []byte) (*StoredData, error) {
	data := &StoredData{}

	r, err := gzip.NewReader(bytes.NewBuffer(value))
	if err != nil {
		return data, err
	}

	acmeData, err := ioutil.ReadAll(r)
	if err != nil {
		return data, err
	}

	storedData := &StoredData{}
	if err := json.Unmarshal(acmeData, &storedData); err != nil {
		return data, err
	}

	return storedData, nil
}

// KVBackend represents a Key/Value pair backend
type KVBackend struct {
	Name   string
	Client []string
	Config *store.Config
}

func register(backend string) (store.Backend, error) {
	switch backend {
	case CONSUL:
		consul.Register()
		return store.CONSUL, nil
	case ETCD:
		etcdv3.Register()
		return store.ETCDV3, nil
	case ZOOKEEPER:
		zookeeper.Register()
		return store.ZK, nil
	case BOLTDB:
		boltdb.Register()
		return store.BOLTDB, nil
	default:
		return "", fmt.Errorf("no backend found for %v", backend)
	}
}

func loopKV(watch bool, kvstore store.Store, dataCh chan *StoredData, errCh chan error) {
	stopCh := make(<-chan struct{})
	events, err := kvstore.Watch(storeKey, stopCh, nil)
	if err != nil {
		errCh <- err
	}
	for {
		kvpair := <-events
		if kvpair == nil {
			errCh <- fmt.Errorf("could not fetch Key/Value pair for key %v", storeKey)
			return
		}
		dataCh <- extractStoredData(kvpair, errCh)
		if !watch {
			close(dataCh)
			close(errCh)
		}
	}
}

func extractStoredData(kvpair *store.KVPair, errCh chan error) *StoredData {
	storedData, err := getStoredDataFromGzip(kvpair.Value)
	if err != nil {
		errCh <- err
	}
	return storedData
}

func getSingleData(kvstore store.Store, dataCh chan *StoredData, errCh chan error) {
	kvpair, err := kvstore.Get(storeKey, nil)
	if err != nil {
		errCh <- err
		return
	}
	if kvpair == nil {
		errCh <- fmt.Errorf("could not fetch Key/Value pair for key %v", storeKey)
		return
	}
	dataCh <- extractStoredData(kvpair, errCh)
	close(dataCh)
	close(errCh)
}

func (b KVBackend) getStoredData(watch bool) (<-chan *StoredData, <-chan error) {

	dataCh := make(chan *StoredData)
	errCh := make(chan error)

	backend, err := register(b.Name)
	if err != nil {
		go func() {
			errCh <- err
		}()
		return dataCh, errCh
	}
	kvstore, err := valkeyrie.NewStore(
		backend,
		b.Client,
		b.Config,
	)

	if err != nil {
		go func() {
			errCh <- err
		}()
		return dataCh, errCh
	}

	if !watch {
		go getSingleData(kvstore, dataCh, errCh)
		return dataCh, errCh
	}

	go loopKV(watch, kvstore, dataCh, errCh)

	return dataCh, errCh

}