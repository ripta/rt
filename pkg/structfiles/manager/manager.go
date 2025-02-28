package manager

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

var ErrUnknownFormat = errors.New("unknown format")

// Manager is a file processor.
//
//   - Read documents from a file or a reader. Each source becomes a group of documents.
//   - A group has some metadata.
//   - Optionally, sort the documents.
//   - Optionally, regroup the documents.
//   - Write the documents to a file or a writer. Each group becomes a single
//     instance of a writer. Up to the writer to determine how multiple
//     documents are written out.
type Manager struct {
	Version string           `json:"version"`
	Groups  []*DocumentGroup `json:"document_groups"`
}

func New() *Manager {
	return &Manager{
		Version: "v1.0",
		Groups:  []*DocumentGroup{},
	}
}

func (m *Manager) ProcessAll(files []string) error {
	if len(files) == 0 {
		return nil
	}

	errs := []error{}
	for _, file := range files {
		if file == "-" {
			if err := m.ProcessReader("stdin://", os.Stdin); err != nil {
				errs = append(errs, err)
			}
			continue
		}

		fi, err := os.Stat(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if fi.IsDir() {
			if err := m.ProcessDir(file); err != nil {
				errs = append(errs, err)
			}
			continue
		}

		if err := m.ProcessFile(file); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	return errors.Join(errs...)
}

func (m *Manager) ProcessDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}

	if !fi.IsDir() {
		return m.ProcessFile(dir)
	}

	errs := []error{}
	fsys := os.DirFS(dir)

	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		dis, err := processFile(filepath.Join(dir, path))
		if err != nil {
			errs = append(errs, err)
			return nil
		}

		m.Groups = append(m.Groups, &DocumentGroup{
			Name:      dir,
			Documents: dis,
		})

		return nil
	})

	return errors.Join(errs...)
}

func (m *Manager) ProcessFile(file string) error {
	dis, err := processFile(file)
	if err != nil {
		return err
	}

	m.Groups = append(m.Groups, &DocumentGroup{
		Name:      file,
		Documents: dis,
	})
	return nil
}

func (m *Manager) ProcessReader(name string, in io.Reader) error {
	docs, err := loadFrom(in, "")
	if err != nil {
		return fmt.Errorf("loading documents from reader for %q: %w", name, err)
	}

	dis := []*DocumentInfo{}
	for i, doc := range docs {
		dis = append(dis, &DocumentInfo{
			SrcName:  name,
			SrcIndex: i,
			Document: doc,
		})
	}

	m.Groups = append(m.Groups, &DocumentGroup{
		Name:      name,
		Documents: dis,
	})

	return nil
}

func processFile(file string) ([]*DocumentInfo, error) {
	in, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	docs, err := loadFrom(in, FindByExtension(filepath.Ext(file)))
	if err != nil {
		return nil, fmt.Errorf("loading documents from file %q: %w", file, err)
	}

	dis := []*DocumentInfo{}
	for i, doc := range docs {
		dis = append(dis, &DocumentInfo{
			SrcName:  file,
			SrcIndex: i,
			Document: doc,
		})
	}

	return dis, nil
}

// loadFrom reads documents from a reader in a specific format. The reader is
// read until EOF. An EOF is not an error. The documents are returned in the
// order they were read.
//
// The format may be any valid format as returned by GetFormats(), or the special
// empty string, which will try every format in turn.
func loadFrom(in io.Reader, format string) ([]*Document, error) {
	df := GetDecoderFactory(format)
	if df == nil {
		if format == "" {
			df = AutoDecoder
		} else {
			return nil, fmt.Errorf("%w %q", ErrUnknownFormat, format)
		}
	}

	dec := df(in)

	docs := []*Document{}
	for {
		var doc Document
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		docs = append(docs, &doc)
	}

	return docs, nil
}

func (m *Manager) Copy() *Manager {
	gs := []*DocumentGroup{}
	for _, g := range m.Groups {
		dis := []*DocumentInfo{}
		for _, di := range g.Documents {
			d := *di.Document
			dis = append(dis, &DocumentInfo{
				SrcName:  di.SrcName,
				SrcIndex: di.SrcIndex,
				Document: &d,
			})
		}

		gs = append(gs, &DocumentGroup{
			Name:      g.Name,
			Documents: dis,
		})
	}

	return &Manager{
		Groups: gs,
	}
}

// AllInOne merges documents from all groups into a single group.
func (m *Manager) AllInOne() error {
	dg := &DocumentGroup{
		Name:      "all-in-one",
		Documents: []*DocumentInfo{},
	}

	for _, g := range m.Groups {
		dg.Documents = append(dg.Documents, g.Documents...)
	}

	m.Groups = []*DocumentGroup{
		dg,
	}

	return nil
}

var (
	anySliceType = reflect.TypeOf([]any{})
	boolType     = reflect.TypeOf(true)
	intType      = reflect.TypeOf(0)
	stringType   = reflect.TypeOf("")
)

// GroupBy regroups documents based on the result of evaluating the expression.
// The document is available as `doc` in the expression.
func (m *Manager) GroupBy(expr string) error {
	opts := []cel.EnvOption{}
	opts = append(opts, cel.Variable("doc", cel.DynType))
	opts = append(opts, cel.Variable("index", cel.IntType))

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return err
	}

	ast, res := env.Compile(expr)
	if res != nil && res.Err() != nil {
		return res.Err()
	}

	prog, err := env.Program(ast)
	if err != nil {
		return err
	}

	groups := map[string]*DocumentGroup{}
	var errs []error
	var index int
	for _, g := range m.Groups {
		for i, di := range g.Documents {
			val, _, err := prog.Eval(map[string]any{
				"doc":   *di.Document,
				"index": index,
				"source": map[string]any{
					"name":  di.SrcName,
					"index": di.SrcIndex,
				},
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("evaluating expression %q for document %d in group %s: %w", expr, i, g.Name, err))
				continue
			}

			index++

			key, err := val.ConvertToType(types.StringType).ConvertToNative(stringType)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			k := key.(string)
			if _, ok := groups[k]; !ok {
				groups[k] = &DocumentGroup{
					Name:      k,
					Documents: []*DocumentInfo{},
				}
			}

			groups[k].Documents = append(groups[k].Documents, di)
		}
	}

	names := []string{}
	for k := range groups {
		names = append(names, k)
	}
	sort.Strings(names)

	m.Groups = []*DocumentGroup{}
	for _, name := range names {
		m.Groups = append(m.Groups, groups[name])
	}

	return errors.Join(errs...)
}

// SortByFunc sorts documents in each group based on the result of evaluating
// the expression. The value of the document is available as `doc`. See also
// SortBy.
func (m *Manager) SortByFunc(expr string, reverse bool) error {
	if reverse {
		return m.SortBy(fmt.Sprintf(`b.%s < a.%s`, expr, expr))
	}
	return m.SortBy(fmt.Sprintf(`a.%s < b.%s`, expr, expr))
}

// SortBy sorts documents in each group based on the result of evaluating
// the expression. The two documents being compared are available as `a.doc`
// and `b.doc`. See also SortByFunc.
func (m *Manager) SortBy(expr string) error {
	objA := cel.Variable("a", cel.MapType(cel.StringType, cel.DynType))
	objB := cel.Variable("b", cel.MapType(cel.StringType, cel.DynType))
	env, err := cel.NewEnv(objA, objB)
	if err != nil {
		return err
	}

	ast, res := env.Compile(expr)
	if res != nil && res.Err() != nil {
		return res.Err()
	}

	prog, err := env.Program(ast)
	if err != nil {
		return err
	}

	var errs []error
	for _, g := range m.Groups {
		sort.Slice(g.Documents, func(i, j int) bool {
			v1 := map[string]any{
				"doc": *g.Documents[i].Document,
				"source": map[string]any{
					"name":  g.Documents[i].SrcName,
					"index": g.Documents[i].SrcIndex,
				},
			}

			v2 := map[string]any{
				"doc": *g.Documents[j].Document,
				"source": map[string]any{
					"name":  g.Documents[j].SrcName,
					"index": g.Documents[j].SrcIndex,
				},
			}

			val, _, err := prog.Eval(map[string]any{
				"a": v1,
				"b": v2,
			})
			if err != nil {
				errs = append(errs, err)
				return false
			}

			b, err := val.ConvertToNative(boolType)
			if err != nil {
				errs = append(errs, err)
				return false
			}

			return b.(bool)
		})
	}

	return errors.Join(errs...)
}

func (m *Manager) Emit(wcf WriteCloserFactory, format string) error {
	for _, g := range m.Groups {
		if err := dumpTo(wcf, format, g); err != nil {
			return fmt.Errorf("writing group %q: %w", g.Name, err)
		}
	}

	return nil
}

func dumpTo(wcf WriteCloserFactory, format string, dg *DocumentGroup) error {
	out, err := wcf(dg)
	if err != nil {
		return err
	}

	if format == "" {
		return fmt.Errorf("%w: no format specified", ErrUnknownFormat)
	}

	df := GetEncoderFactory(format)
	if df == nil {
		return fmt.Errorf("%w %q", ErrUnknownFormat, format)
	}

	enc, finalize := df(out)
	defer finalize()

	for _, di := range dg.Documents {
		if err := enc.Encode(di.Document); err != nil {
			return err
		}
	}

	return nil
}
