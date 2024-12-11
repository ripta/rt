package sanik8s

import (
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func Load(r io.Reader) error {
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		return err
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	d := scheme.Codecs.UniversalDeserializer()
	d.Decode(raw, nil, nil)

	s.Default()

	p := corev1.Pod{}

}
