package fixture

type Paren (struct {
	//kama:partial:paren
	X int
})

func H() {} //kama:partial funcline
//kama:partial next line

func I() {
} //kama:partial funcend

type (
	G3 int
) //kama:partial groupline

var W = 1 //kama:partial varline
