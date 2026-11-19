package model

// Shelf holds collections of targets under the default strategies.
type Shelf struct {
	// Unnamed slices and maps of targets map as a whole.
	Books    []Book           `json:"books"`
	BookRefs []*Book          `json:"book_refs"`
	ByID     map[string]Book  `json:"by_id"`
	RefsByID map[string]*Book `json:"refs_by_id"`

	List    Books      `json:"list"`
	RefList BookRefs   `json:"ref_list"`
	Map     BookMap    `json:"map"`
	RefMap  BookRefMap `json:"ref_map"`

	ListPtr   *Books      `json:"list_ptr"`
	RefMapPtr *BookRefMap `json:"ref_map_ptr"`

	Sections Sections      `json:"sections"`
	Pages    Pages[string] `json:"pages"`
}

// Catalog holds the collections of Shelf with merge directives.
type Catalog struct {
	Books    []Book           `json:"books"`
	BookRefs []*Book          `json:"book_refs"`
	ByID     map[string]Book  `json:"by_id"`
	RefsByID map[string]*Book `json:"refs_by_id"`
	List     Books            `json:"list"`
	RefMap   BookRefMap       `json:"ref_map"`
	Map      BookMap          `json:"map"`
	ListPtr  *Books           `json:"list_ptr"`
	Sections Sections         `json:"sections"`
}

// Book is reachable through the collections of Shelf.
type Book struct {
	Title string `json:"title"`
	Pages *int   `json:"pages"`
}

// Books is a slice target of a struct target.
type Books []Book

// BookRefs is a slice target of pointers to a struct target.
type BookRefs []*Book

// BookMap is a map target of a struct target.
type BookMap map[string]Book

// BookRefMap is a map target of pointers to a struct target.
type BookRefMap map[string]*Book

// Section holds collection targets and is the element of Sections.
type Section struct {
	Name  string     `json:"name"`
	Books Books      `json:"books"`
	Refs  BookRefMap `json:"refs"`
}

// Sections is a slice target whose element holds collection targets.
type Sections []Section

// Pages is a generic slice target.
type Pages[T any] []Page[T]

// Page is reachable through the element of Pages.
type Page[T any] struct {
	Body T `json:"body"`
}
