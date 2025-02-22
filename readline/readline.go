package readline

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
)

type Prompt struct {
	Prompt         string
	AltPrompt      string
	Placeholder    string
	AltPlaceholder string
	UseAlt         bool
}

type Terminal struct {
	outchan chan rune
}

type Instance struct {
	Prompt   *Prompt
	Terminal *Terminal
	History  *History
}

func New(prompt Prompt) (*Instance, error) {
	term, err := NewTerminal()
	if err != nil {
		return nil, err
	}

	history, err := NewHistory()
	if err != nil {
		return nil, err
	}

	return &Instance{
		Prompt:   &prompt,
		Terminal: term,
		History:  history,
	}, nil
}

func (i *Instance) Readline() (string, error) {
	prompt := i.Prompt.Prompt
	if i.Prompt.UseAlt {
		prompt = i.Prompt.AltPrompt
	}
	fmt.Print(prompt)

	fd := int(syscall.Stdin)
	termios, err := SetRawMode(fd)
	if err != nil {
		return "", err
	}
	defer UnsetRawMode(fd, termios)

	buf, _ := NewBuffer(i.Prompt)

	var esc bool
	var escex bool
	var metaDel bool
	var pasteMode PasteMode

	var currentLineBuf []rune

	for {
		if buf.IsEmpty() {
			ph := i.Prompt.Placeholder
			if i.Prompt.UseAlt {
				ph = i.Prompt.AltPlaceholder
			}
			fmt.Printf(ColorGrey + ph + fmt.Sprintf(CursorLeftN, len(ph)) + ColorDefault)
		}

		r, err := i.Terminal.Read()

		if buf.IsEmpty() {
			fmt.Print(ClearToEOL)
		}

		if err != nil {
			return "", io.EOF
		}

		if escex {
			escex = false

			switch r {
			case KeyUp:
				if i.History.Pos > 0 {
					if i.History.Pos == i.History.Size() {
						currentLineBuf = []rune(buf.String())
					}
					buf.Replace(i.History.Prev())
				}
			case KeyDown:
				if i.History.Pos < i.History.Size() {
					buf.Replace(i.History.Next())
					if i.History.Pos == i.History.Size() {
						buf.Replace(currentLineBuf)
					}
				}
			case KeyLeft:
				buf.MoveLeft()
			case KeyRight:
				buf.MoveRight()
			case CharBracketedPaste:
				var code string
				for cnt := 0; cnt < 3; cnt++ {
					r, err = i.Terminal.Read()
					if err != nil {
						return "", io.EOF
					}

					code += string(r)
				}
				if code == CharBracketedPasteStart {
					pasteMode = PasteModeStart
				} else if code == CharBracketedPasteEnd {
					pasteMode = PasteModeEnd
				}
			case KeyDel:
				if buf.Size() > 0 {
					buf.Delete()
				}
				metaDel = true
			case MetaStart:
				buf.MoveToStart()
			case MetaEnd:
				buf.MoveToEnd()
			default:
				// skip any keys we don't know about
				continue
			}
			continue
		} else if esc {
			esc = false

			switch r {
			case 'b':
				buf.MoveLeftWord()
			case 'f':
				buf.MoveRightWord()
			case CharEscapeEx:
				escex = true
			}
			continue
		}

		switch r {
		case CharNull:
			continue
		case CharEsc:
			esc = true
		case CharInterrupt:
			return "", ErrInterrupt
		case CharLineStart:
			buf.MoveToStart()
		case CharLineEnd:
			buf.MoveToEnd()
		case CharBackward:
			buf.MoveLeft()
		case CharForward:
			buf.MoveRight()
		case CharBackspace, CharCtrlH:
			buf.Remove()
		case CharTab:
			// todo: convert back to real tabs
			for cnt := 0; cnt < 8; cnt++ {
				buf.Add(' ')
			}
		case CharDelete:
			if buf.Size() > 0 {
				buf.Delete()
			} else {
				return "", io.EOF
			}
		case CharKill:
			buf.DeleteRemaining()
		case CharCtrlU:
			buf.DeleteBefore()
		case CharCtrlL:
			buf.ClearScreen()
		case CharCtrlW:
			buf.DeleteWord()
		case CharEnter:
			output := buf.String()
			if output != "" {
				i.History.Add([]rune(output))
			}
			buf.MoveToEnd()
			fmt.Println()
			switch pasteMode {
			case PasteModeStart:
				output = `"""` + output
			case PasteModeEnd:
				output = output + `"""`
			}
			return output, nil
		default:
			if metaDel {
				metaDel = false
				continue
			}
			if r >= CharSpace || r == CharEnter {
				buf.Add(r)
			}
		}
	}
}

func (i *Instance) HistoryEnable() {
	i.History.Enabled = true
}

func (i *Instance) HistoryDisable() {
	i.History.Enabled = false
}

func NewTerminal() (*Terminal, error) {
	t := &Terminal{
		outchan: make(chan rune),
	}

	go t.ioloop()

	return t, nil
}

func (t *Terminal) ioloop() {
	buf := bufio.NewReader(os.Stdin)

	for {
		r, _, err := buf.ReadRune()
		if err != nil {
			close(t.outchan)
			break
		}
		t.outchan <- r
	}
}

func (t *Terminal) Read() (rune, error) {
	r, ok := <-t.outchan
	if !ok {
		return 0, io.EOF
	}

	return r, nil
}
