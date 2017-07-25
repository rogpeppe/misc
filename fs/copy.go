package fs

// Copy copies one item and all its contents from src to
// dst. It reports whether the stream is still active
// (the caller should quit if not).
func Copy(ctx Context, dst chan<- Item, src <-chan Item) bool {
	depth := 1
	reply := make(chan Answer)

	for it := range src {
		dst <- it.WithReply(reply)
		r := <-reply
		it.Reply <- r
		switch r {
		case Quit:
			return false
		case Next:
			if it.IsEnd() {
				if depth--; depth == 0 {
					return true
				}
			}
		case Skip:
			if depth--; depth == 0 {
				return true
			}
		case Down:
			if !it.IsEnd() {
				depth++
			}
		default:
			panic("unexpected answer")
		}
	}
	return false
}
