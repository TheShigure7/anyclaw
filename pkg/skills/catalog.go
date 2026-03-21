package skills

type Skill struct {
	Name        string
	Description string
	Version     string
	Entrypoint  string
}

type Catalog struct {
	Items []Skill
}

func NewCatalog() *Catalog {
	return &Catalog{Items: []Skill{}}
}
