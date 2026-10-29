package system

// FirewallStatus 防火牆會不會擋住手機
type FirewallStatus struct {
	// Kind ufw、firewalld、windows；空的代表沒看到防火牆
	Kind string `json:"kind"`
	// Open yes：已經放行；no：沒放行；unknown：看不到規則（Linux 一般使用者讀不到）
	Open string `json:"open"`
	// Command Linux：要使用者自己執行的放行指令（要 sudo）
	Command string `json:"command,omitempty"`
	// CanFix Windows：可以按按鈕放行（會跳出系統的權限確認）
	CanFix bool `json:"canFix,omitempty"`
	// PublicNetwork Windows 把目前的網路當成「公用網路」，家用規則不適用
	PublicNetwork bool `json:"publicNetwork,omitempty"`
}

// RuleName Windows 防火牆規則的名稱
const RuleName = "Local AI Helper"
