package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"os"

	"github.com/golang/freetype/truetype"
	"github.com/inconshreveable/log15"
	"github.com/rs/xid"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

type generateRequest struct {
	r            *http.Request
	logger       log15.Logger
	descriptions map[string]description

	base     string
	question string
	answers  []string

	uid   string
	err   error
	desc  description
	image *image.RGBA
	font  *truetype.Font
}

func (r *generateRequest) init() {
	r.uid = xid.New().String()
	r.logger = r.logger.New("uid", r.uid)
}

func (r *generateRequest) readPayload() {
	if r.err != nil {
		return
	}

	r.r.ParseForm()

	r.base = r.r.Form.Get("base")
	if r.base == "" {
		r.base = "qvgdm"
	}

	r.question = r.r.Form.Get("question")
	r.answers = r.r.Form["answers"]

	var ok bool
	r.desc, ok = r.descriptions[r.base]
	if !ok {
		r.err = fmt.Errorf(`unknown base %q`, r.base)
		return
	}
}

// getBase open and decode the base image, and convert it into a RGBA image
// suitable to be modified.
func (r *generateRequest) getBase() {
	if r.err != nil {
		return
	}

	f, err := os.Open(r.desc.Base)
	if err != nil {
		r.err = wrap(err, "opening base image")
		return
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		r.err = wrap(err, "decoding base image")
		return
	}

	b := src.Bounds()
	r.image = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(r.image, r.image.Bounds(), src, b.Min, draw.Src)
}

// Get the font for this image.
func (r *generateRequest) getFont() {
	if r.err != nil {
		return
	}

	var err error
	r.font, err = truetype.Parse(goregular.TTF)
	if err != nil {
		r.err = wrap(err, "loading font")
		return
	}
}

func (r *generateRequest) writeQuestion() {
	if r.err != nil {
		return
	}

	// Draw the question.
	d := &font.Drawer{
		Dst: r.image,
		Src: image.NewUniform(color.White),
		Face: truetype.NewFace(r.font, &truetype.Options{
			Size: r.desc.Question.Size,
		}),
		Dot: fixed.P(r.desc.Question.X, r.desc.Question.Y),
	}
	d.DrawString(r.question)
}

func (r *generateRequest) writeAnswers() {
	if r.err != nil {
		return
	}

	// Draw the answers.
	for i := 0; i < len(r.answers) && i < len(r.desc.Answers); i++ {
		d := &font.Drawer{
			Dst: r.image,
			Src: image.NewUniform(color.White),
			Face: truetype.NewFace(r.font, &truetype.Options{
				Size: r.desc.Answers[i].Size,
			}),
			Dot: fixed.P(r.desc.Answers[i].X, r.desc.Answers[i].Y),
		}
		d.DrawString(r.answers[i])
	}
}
