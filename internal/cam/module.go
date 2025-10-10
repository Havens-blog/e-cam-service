package cam

type Module struct {
	Hdl        *Handler
	Svc        Service
	AccountSvc CloudAccountService
}
