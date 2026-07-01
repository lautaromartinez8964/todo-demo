package main

type RasterImage struct {
	Width  int
	Height int
}

// 场景1： 返回局部变量的指针

func createSharedImage() *RasterImage {
	img := RasterImage{Width: 1024, Height: 1024}
	return &img // 函数结束了，但要把指针传出去，外部还要用它！
}

// 场景 2：普通局部变量，不传出去
func createLocalImage() {
	img := RasterImage{Width: 512, Height: 512}
	_ = img.Width // 就在内部用一下，函数结束就没人要了
}

func man() {
	_ = createSharedImage()
	createLocalImage()
}
