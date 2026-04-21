package channels

import "strings"

func streamWithMessageFallback(streamFn func(onChunk func(chunk string)) error, sendFinal func(text string) error) error {
	var accumulated strings.Builder
	err := streamFn(func(chunk string) {
		accumulated.WriteString(chunk)
	})

	final := accumulated.String()
	if err != nil {
		if strings.TrimSpace(final) != "" {
			_ = sendFinal(final + "\n\n[Error: " + err.Error() + "]")
		}
		return err
	}
	if strings.TrimSpace(final) == "" {
		return nil
	}
	return sendFinal(final)
}
