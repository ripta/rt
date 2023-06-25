package decoder

import (
	"encoding/json"
	"io"
	"reflect"
	"sync"
)

func NewStream(r io.Reader, splitter SplitterFunc) *Stream {
	return &Stream{
		dec:      json.NewDecoder(r),
		ibuf:     []any{},
		splitter: splitter,
	}
}

type Stream struct {
	dec      *json.Decoder
	ibuf     []any
	imut     sync.RWMutex
	splitter SplitterFunc
}

func (s *Stream) Decode(obj *any) error {
	// ensure obj is a pointer
	rv := reflect.ValueOf(obj)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &json.InvalidUnmarshalError{
			Type: reflect.TypeOf(obj),
		}
	}

	s.imut.Lock()
	defer s.imut.Unlock()

	if len(s.ibuf) > 0 {
		// read from front to maintain order
		*obj = s.ibuf[0]
		s.ibuf = s.ibuf[1:]
		return nil
	}

	var buf any
	if err := s.dec.Decode(&buf); err != nil {
		return err
	}

	if s.splitter == nil {
		*obj = buf
		return nil
	}

	ibuf, ok := s.splitter(buf)
	if !ok {
		*obj = buf
		return nil
	}

	*obj = ibuf[0]
	s.ibuf = ibuf[1:]
	return nil
}

func (s *Stream) More() bool {
	s.imut.RLock()
	defer s.imut.RUnlock()
	return len(s.ibuf) > 0 || s.dec.More()
}
