package clt

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	success int = iota
	fail
)

const (
	spinner int = iota
	bar
	loading
)

// Spinner is a set of unicode strings that show a moving progress indication in the terminal
type Spinner []string

var (
	// Wheel created with pipes and slashes
	Wheel Spinner = []string{"|", "/", "-", "\\"}
	// Bouncing dots
	Bouncing Spinner = []string{"⠁", "⠂", "⠄", "⠂"}
	// Clock that spins two hours per step
	Clock Spinner = []string{"🕐 ", "🕑 ", "🕒 ", "🕓 ", "🕔 ", "🕕 ", "🕖 ", "🕗 ", "🕘 ", "🕙 ", "🕚 "}
	// Dots that spin around a rectangle
	Dots Spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// Progress structure used to render progress and loading indicators
type Progress struct {
	// Prompt to display before spinner or bar
	Prompt string
	// Approximate length of the total progress display, including
	// the prompt and the ..., does not include status indicator
	// at the end (e.g, the spinner, FAIL, OK, or XX%)
	DisplayLength int

	style     int
	cf        chan float64
	c         chan int
	spinsteps Spinner
	delay     time.Duration
	output    io.Writer
	wg        sync.WaitGroup
	mtx       sync.Mutex
}

// NewProgressSpinner returns a new spinner with prompt <message>
// display length defaults to 30.
func NewProgressSpinner(format string, args ...interface{}) *Progress {
	return &Progress{
		style:         spinner,
		Prompt:        fmt.Sprintf(format, args...),
		DisplayLength: 30,
		output:        os.Stdout,
		spinsteps:     Wheel,
	}
}

// NewProgressBar returns a new progress bar with prompt <message>
// display length defaults to 20
func NewProgressBar(format string, args ...interface{}) *Progress {
	return &Progress{
		style:         bar,
		Prompt:        fmt.Sprintf(format, args...),
		DisplayLength: 20,
		output:        os.Stdout,
	}
}

// NewLoadingMessage creates a spinning loading indicator followed by a message.
// The loading indicator does not indicate sucess or failure and disappears when
// you call either Success() or Failure().  This is useful to show action when
// making remote calls that are expected to be short.  The delay parameter is to
// prevent flickering when the remote call finishes quickly.  If you finish your call
// and call Success() or Failure() within the delay period, the loading indicator
// will never be shown.
func NewLoadingMessage(message string, spinner Spinner, delay time.Duration) *Progress {
	return &Progress{
		style:         loading,
		Prompt:        message,
		DisplayLength: 0,
		spinsteps:     spinner,
		output:        os.Stdout,
		delay:         delay,
	}
}

// Start launches a Goroutine to render the progress bar or spinner
// and returns control to the caller for further processing.  Spinner
// will update automatically every 250ms until Success() or Fail() is
// called.  Bars will update by calling Update(<pct_complete>).  You
// must always finally call either Success() or Fail() to terminate
// the go routine.
func (p *Progress) Start() {
	p.wg.Add(1)
	switch p.style {
	case spinner:
		p.c = make(chan int)
		go renderSpinner(p, p.c)
	case bar:
		p.cf = make(chan float64, 2)
		go renderBar(p, p.cf)
		p.cf <- 0.0
	case loading:
		p.c = make(chan int)
		go renderLoading(p, p.c)
	}
}

// Success should be called on a progress bar or spinner
// after completion is successful
func (p *Progress) Success() {
	switch p.style {
	case spinner:
		p.c <- success
	case bar:
		p.cf <- -1.0
	case loading:
		p.c <- success
	}

	p.wg.Wait()

	switch p.style {
	case spinner:
		close(p.c)
	case bar:
		close(p.cf)
	case loading:
		close(p.c)
	}
}

// Fail should be called on a progress bar or spinner
// if a failure occurs
func (p *Progress) Fail() {
	switch p.style {
	case spinner:
		p.c <- fail
	case bar:
		p.cf <- -2.0
	// loading only has one termination state
	case loading:
		p.c <- success
	}

	p.wg.Wait()

	switch p.style {
	case spinner:
		close(p.c)
	case bar:
		close(p.cf)
	case loading:
		close(p.c)
	}
}

// Start launches a Goroutine to render the progress bar or spinner
// and returns control to the caller for further processing.  Spinner
// will update automatically every 250ms until Success() or Fail() is
// called.  Bars will update by calling Update(<pct_complete>).  You
// must always finally call either Success() or Fail() to terminate
// the go routine.
func (p *Progress) UpdatePrompt(prompt string) {
	p.wg.Add(1)
	defer p.wg.Done()
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.Prompt = prompt
}

func renderSpinner(p *Progress, c chan int) {
	defer p.wg.Done()
	if p.output == nil {
		p.output = os.Stdout
	}
	p.mtx.Lock()
	promptLen := len(p.Prompt)
	p.mtx.Unlock()
	dotLen := p.DisplayLength - promptLen
	if dotLen < 3 {
		dotLen = 3
	}
	for i := 0; ; i++ {
		select {
		case result := <-c:
			switch result {
			case success:
				p.mtx.Lock()
				fmt.Fprintf(p.output, "\x1b[?25h\r%s[%s]\n", p.Prompt, Styled(Green).ApplyTo("OK"))
				p.mtx.Unlock()
			case fail:
				p.mtx.Lock()
				fmt.Fprintf(p.output, "\x1b[?25h\r%s[%s]\n", p.Prompt, Styled(Red).ApplyTo("FAIL"))
				p.mtx.Unlock()
			}
			return
		default:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s[%s]", p.Prompt, spinLookup(i, p.spinsteps))
			p.mtx.Unlock()
			time.Sleep(time.Duration(100) * time.Millisecond)
		}
	}
}

func renderLoading(p *Progress, c chan int) {
	defer p.wg.Done()
	if p.output == nil {
		p.output = os.Stdout
	}

	// delay to prevent flickering
	// calling Success or Failure within delay will shortcircuit the loading indicator
	if p.delay > 0 {
		t := time.NewTicker(p.delay)
		select {
		case <-c:
			return
		case <-t.C:
			t.Stop()
		}
	}

	for i := 0; ; i++ {
		select {
		case <-c:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s\r\n", strings.Repeat(" ", len(p.spinsteps[0])+len(p.Prompt)+3))
			p.mtx.Unlock()
			return
		default:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s  %s", spinLookup(i, p.spinsteps), p.Prompt)
			p.mtx.Unlock()
			time.Sleep(time.Duration(250) * time.Millisecond)
		}
	}
}

func spinLookup(i int, steps []string) string {
	return steps[i%len(steps)]
}

func renderBar(p *Progress, c chan float64) {
	defer p.wg.Done()
	if p.output == nil {
		p.output = os.Stdout
	}

	for result := range c {
		eqLen := int(result * float64(p.DisplayLength))
		spLen := p.DisplayLength - eqLen
		switch {
		case result == -1.0:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s: [%s] %s", p.Prompt, strings.Repeat("=", p.DisplayLength), Styled(Green).ApplyTo("100%"))
			p.mtx.Unlock()
			fmt.Fprintf(p.output, "\x1b[?25h\n")
			return
		case result == -2.0:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s: [%s] %s", p.Prompt, strings.Repeat("X", p.DisplayLength), Styled(Red).ApplyTo("FAIL"))
			p.mtx.Unlock()
			fmt.Fprintf(p.output, "\x1b[?25h\n")
			return
		case result >= 0.0:
			p.mtx.Lock()
			fmt.Fprintf(p.output, "\x1b[?25l\r%s: [%s%s] %2.0f%%", p.Prompt, strings.Repeat("=", eqLen), strings.Repeat(" ", spLen), 100.0*result)
			p.mtx.Unlock()
		}

	}
}

// Update the progress bar using a number [0, 1.0] to represent
// the percentage complete
func (p *Progress) Update(pct float64) {
	p.wg.Add(1)
	defer p.wg.Done()
	if pct >= 1.0 {
		pct = 1.0
	}
	p.cf <- pct
}
