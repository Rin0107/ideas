package app

// must はテストの中だけで使う: 起きないはずのエラーなら panic する。
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
