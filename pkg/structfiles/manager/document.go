package manager

type Document any

type DocumentInfo struct {
	SrcName  string    `json:"source_name"`
	SrcIndex int       `json:"source_index"`
	Document *Document `json:"document"`
}

type DocumentGroup struct {
	Name      string          `json:"name"`
	Documents []*DocumentInfo `json:"documents"`
}
