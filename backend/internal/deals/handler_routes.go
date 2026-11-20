package deals

import "github.com/gin-gonic/gin"

func RegisterRoutes(router gin.IRouter, h *Handler) {
	group := router.Group("/deals")

	group.POST("", h.CreateDeal)
	group.GET("", h.ListDeals)
	group.GET("/:dealID", h.GetDealByID)
	group.GET("/:dealID/payment", h.CheckPayment)
	group.PATCH("/:dealID/status", h.UpdateDealStatus)
	group.POST("/:dealID/submit", h.SubmitWork)
	group.POST("/:dealID/approve", h.ApproveDeal)
	group.POST("/:dealID/dispute", h.DisputeDeal)
	group.PATCH("/:dealID/payee-invoice", h.UpdatePayeeInvoice)
	group.POST("/:dealID/share-link", h.RotateShareLink)
	group.POST("/:dealID/milestones", h.CreateMilestone)
	group.GET("/:dealID/milestones", h.ListMilestones)
	group.PATCH("/:dealID/milestones/:milestoneID", h.UpdateMilestone)
	group.POST("/:dealID/exit", h.ExitDeal)
	group.POST("/wallet/invoice", h.GenerateInvoice)
}

// RegisterPublicRoutes registers the public (unauthenticated) deal routes.
// These are mounted on the root router, not the protected group.
func RegisterPublicRoutes(router gin.IRouter, h *Handler) {
	public := router.Group("/public")
	public.GET("/deals/:shareToken", h.GetPublicDeal)
	public.GET("/deals/:shareToken/milestones", h.PublicListMilestones)
	public.POST("/deals/:shareToken/fund", h.FundDeal)
	public.POST("/deals/:shareToken/approve", h.PublicApproveDeal)
	public.POST("/deals/:shareToken/payout/reset", h.PublicResetPayout)
	public.POST("/deals/:shareToken/milestones/:milestoneID/accept-next", h.PublicAcceptNextMilestone)
	public.POST("/deals/:shareToken/milestones/:milestoneID/reject", h.PublicRejectMilestone)
	public.POST("/deals/:shareToken/exit", h.PublicExitDeal)
}
