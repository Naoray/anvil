package warm

import "golang.org/x/sys/windows"

// platformPathForms returns the 8.3 short and the long name of an existing
// path. A form Windows cannot produce (e.g. 8.3 names disabled) is omitted.
func platformPathForms(path string) []string {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var forms []string
	for _, get := range []func(*uint16, *uint16, uint32) (uint32, error){windows.GetShortPathName, windows.GetLongPathName} {
		buf := make([]uint16, windows.MAX_PATH)
		for {
			n, err := get(name, &buf[0], uint32(len(buf)))
			if err != nil || n == 0 {
				break
			}
			if int(n) > len(buf) {
				buf = make([]uint16, n)
				continue
			}
			forms = append(forms, windows.UTF16ToString(buf[:n]))
			break
		}
	}
	return forms
}
