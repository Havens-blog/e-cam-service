package logquery

import "errors"

// errNilAccounts 装配期缺云账号仓储(fail-fast:无账号源联邦无从谈起)。
var errNilAccounts = errors.New("logquery: cloud account repository is required")
