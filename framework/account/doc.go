// Package account provides a production-oriented account SDK for Gaia services.
//
// The package is intentionally placed under framework/account instead of
// framework/server/accountService: it is a framework-level capability and can be
// used by HTTP, RPC, jobs, or future standalone account services without
// coupling the core account model to Hertz routes.
//
// Framework services can use Gaia's existing MySQL and Redis configuration:
//
//	manager, err := account.NewFramework()
//	if err != nil {
//		return err
//	}
//	if err := manager.Bootstrap(ctx); err != nil {
//		return err
//	}
//
// Services with dedicated account configuration can use:
//
//	manager, err := account.NewFrameworkWithSchema("Account.Mysql", "Account.Redis")
//
// SDK constructors and Bootstrap return errors instead of panicking. Application
// startup code may choose to panic or exit on those errors, but the SDK should
// not decide that policy for its callers.
package account
