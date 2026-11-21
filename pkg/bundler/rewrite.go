package bundler

// edit is a byte-range replacement used while rewriting linked module source.
type edit struct {
	start int
	end   int
	text  string
}
