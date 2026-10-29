package ollama

// detectExtras Windows 先只裝主程式（NVIDIA 顯示卡與 CPU 都能用；AMD 顯示卡要另外的 rocm 套件，還沒做）
func detectExtras(string) []string { return nil }
