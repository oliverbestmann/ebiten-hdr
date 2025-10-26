package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/color"
	"math"
	"time"

	_ "image/jpeg"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

//go:embed gopher.jpg
var _gopher []byte

//go:embed shaders/prelude.kage
var prelude string

var shaderCopySource = prelude + `
	func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
		px := imageSrc0UnsafeAt(srcPos).xyz * color.xyz
		return LogLuvEncode(px)
	}
`

func shaderBlurSource() string {
	return prelude + `
	var Spread float
	var ScaleX float
	var ScaleY float

	func tap(pos vec2, offset float, scale float) vec3 {
		offset *= Spread
	
		// get pixel in lluv format
		lluv := imageSrc0At(pos + vec2(offset * ScaleX, offset * ScaleY))
	
		// convert to rgb
		rgb := LogLuvDecode(lluv)
	
		// and scale by tap
		return rgb * scale
	}
	
	func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
		var res vec3
	
		res += tap(srcPos, -4, 0.05);
		res += tap(srcPos, -3, 0.09);
		res += tap(srcPos, -2, 0.12);
		res += tap(srcPos, -1, 0.15);
		res += tap(srcPos, 0, 0.16);
		res += tap(srcPos, 1, 0.15);
		res += tap(srcPos, 2, 0.12);
		res += tap(srcPos, 3, 0.09);
		res += tap(srcPos, 4, 0.05);
	
		// return logluv value
		return LogLuvEncode(res)
	}

`
}

var shaderThresholdSource = prelude + `
	var LumaThreshold float
	var BloomStrength float

	func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
		lluv := imageSrc0At(srcPos)
		luma := LogLuvLuma(lluv)
		
		if luma < LumaThreshold {
			return vec4(0)
		}

		rgb := LogLuvDecode(lluv) * BloomStrength;
		return LogLuvEncode(rgb) 
	}
`

var shaderTonemapSource = prelude + `
	func uncharted2_tonemap_partial(x vec3) vec3 {
		A := 0.15;
		B := 0.50;
		C := 0.10;
		D := 0.20;
		E := 0.02;
		F := 0.30;
		return ((x*(A*x+C*B)+D*E)/(x*(A*x+B)+D*F))-E/F;
	}
	
	func uncharted2_filmic(v vec3) vec3 {
    	exposure_bias := 2.0
    	curr := uncharted2_tonemap_partial(v * exposure_bias)

		W := vec3(11.2);
		white_scale := vec3(1.0) / uncharted2_tonemap_partial(W)
		return curr * white_scale
	}
	
	func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
		lluv := imageSrc0UnsafeAt(srcPos)
		lluvBloom := imageSrc1UnsafeAt(srcPos)
		
		// merge with bloom
		rgb := LogLuvDecode(lluv) + LogLuvDecode(lluvBloom)
		
		// tone map the rgb value
		mapped := uncharted2_filmic(rgb)
		
		return vec4(mapped, 1)
	}
`

func mustCompile(shaderSource string) *ebiten.Shader {
	shader, err := ebiten.NewShader([]byte(shaderSource))
	if err != nil {
		panic(err)
	}

	return shader
}

var shaderCopy = mustCompile(shaderCopySource)
var shaderBlur = mustCompile(shaderBlurSource())
var shaderThreshold = mustCompile(shaderThresholdSource)
var shaderTonemap = mustCompile(shaderTonemapSource)

var startTime = time.Now()

type Game struct {
	image *ebiten.Image

	temp  *ebiten.Image
	bloom *ebiten.Image
}

func (g *Game) Update() error {
	if g.image == nil {
		image, _, err := ebitenutil.NewImageFromReader(bytes.NewReader(_gopher))
		if err != nil {
			panic(err)
		}

		g.image = image
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {

	for idx := range 5 {
		scale := 10 - float32(idx)*2
		// draw some texture. alpha is ignored
		var dops ebiten.DrawRectShaderOptions
		dops.GeoM.Scale(200, 200)
		dops.GeoM.Translate(200*float64(idx), 300)
		dops.ColorScale.Scale(scale, scale, scale, 1)
		DrawImage(screen, g.image, dops)
	}

	// draw a bright spot
	{
		var dops ebiten.DrawRectShaderOptions
		dops.ColorScale.Scale(10, 1, 10, 1)
		dops.GeoM.Scale(50, 50)
		dops.GeoM.Translate(200, 200)
		DrawImage(screen, whiteImage, dops)
	}

	for scale := range 10 {
		scale += 1
		fScale := float32(scale)

		t := time.Since(startTime).Seconds() * (1 + float64(fScale)/10)

		{
			var dops ebiten.DrawRectShaderOptions
			dops.ColorScale.Scale(0.1*fScale, 1*fScale, 0.5*fScale, 1)
			dops.GeoM.Rotate(t)
			dops.GeoM.Scale(40, 40)
			dops.GeoM.Translate(230+50*float64(fScale), 240+50*math.Sin(t))

			DrawImage(screen, whiteImage, dops)
		}
	}
}

func DrawImage(target *ebiten.Image, source *ebiten.Image, ops ebiten.DrawRectShaderOptions) {
	iw, ih := source.Bounds().Dx(), source.Bounds().Dy()

	g := ops.GeoM

	ops.GeoM.Reset()
	ops.GeoM.Scale(1/float64(iw), 1/float64(ih))
	ops.GeoM.Concat(g)

	ops.Images[0] = source
	ops.Blend = ebiten.BlendCopy

	target.DrawRectShader(iw, ih, shaderCopy, &ops)
}

var whiteImage *ebiten.Image

func init() {
	whiteImage = ebiten.NewImage(1, 1)
	whiteImage.Fill(color.White)
}

func (g *Game) DrawFinalScreen(screen ebiten.FinalScreen, offscreen *ebiten.Image, geoM ebiten.GeoM) {
	w, h := offscreen.Bounds().Dx(), offscreen.Bounds().Dy()

	if g.temp == nil || g.temp.Bounds() != offscreen.Bounds() {
		g.temp = ebiten.NewImage(w, h)
		g.bloom = ebiten.NewImage(w, h)
	}

	{
		// copy out the bloom by thresholding
		var dops ebiten.DrawRectShaderOptions
		dops.Images[0] = offscreen
		dops.Blend = ebiten.BlendCopy
		dops.Uniforms = map[string]any{
			"LumaThreshold": float32(2),
			"BloomStrength": float32(0.2),
		}
		g.bloom.DrawRectShader(w, h, shaderThreshold, &dops)
	}

	for pass := range 5 {
		pass += 1

		{
			// apply h blur to offscreen and write to temp
			var dops ebiten.DrawRectShaderOptions
			dops.Images[0] = g.bloom
			dops.Blend = ebiten.BlendCopy
			dops.Uniforms = map[string]any{
				"Spread": float32(pass),
				"ScaleX": 1.0,
			}
			g.temp.DrawRectShader(w, h, shaderBlur, &dops)
		}

		{
			// apply v blur to temp and write to offscreen
			var dops ebiten.DrawRectShaderOptions
			dops.Images[0] = g.temp
			dops.Blend = ebiten.BlendCopy
			dops.Uniforms = map[string]any{
				"Spread": float32(pass),
				"ScaleY": 1.0,
			}
			g.bloom.DrawRectShader(w, h, shaderBlur, &dops)
		}
	}

	{
		// add bloom to the screen and
		// apply tone mapping to the original scene
		var dops ebiten.DrawRectShaderOptions
		dops.Images[0] = offscreen
		dops.Images[1] = g.bloom
		dops.Blend = ebiten.BlendCopy
		g.temp.DrawRectShader(w, h, shaderTonemap, &dops)
	}

	{
		text := fmt.Sprintf("FPS: %1.2f", ebiten.ActualFPS())
		ebitenutil.DebugPrintAt(g.temp, text, 16, 16)
		ebiten.DefaultDrawFinalScreen(screen, g.temp, geoM)
	}

}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return 1000, 600
}

func main() {
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSize(1000, 600)

	if err := ebiten.RunGame(&Game{}); err != nil {
		panic(err)
	}
}
