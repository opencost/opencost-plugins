package datadog

type ProductVisualResource struct {
	Type string `json:"Type"`
	URL  string `json:"URL"`
}

type ProductOverview struct {
	ProductVisualResource       []ProductVisualResource `json:"ProductVisualResource"`
	Highlights                  []string                `json:"Highlights"`
	Description                 string                  `json:"Description"`
	DataLabelingServicesEnabled bool                    `json:"DataLabelingServicesEnabled"`
	FulfillmentMethod           struct {
		Description string `json:"description"`
		Title       string `json:"title"`
	} `json:"FulfillmentMethod"`
	PrivateLinkEnabled    bool   `json:"PrivateLinkEnabled"`
	ProductImageCDNKey    string `json:"ProductImageCDNKey"`
	Title                 string `json:"Title"`
	ProductRatingOverview struct {
		AverageCustomerRating int `json:"AverageCustomerRating"`
		CustomerReviewsCount  int `json:"CustomerReviewsCount"`
		CustomerRatingCounts  struct {
			Star5 int `json:"Star5"`
			Star4 int `json:"Star4"`
			Star3 int `json:"Star3"`
			Star2 int `json:"Star2"`
			Star1 int `json:"Star1"`
		} `json:"CustomerRatingCounts"`
	} `json:"ProductRatingOverview"`
	ShortDescription string `json:"ShortDescription"`
}

type SellerInformation struct {
	SellerId   string `json:"SellerId"`
	SellerName string `json:"SellerName"`
}

type VendorInsights struct {
	SecurityInsightArn string `json:"SecurityInsightArn"`
}

type VersionInformation struct {
	DateSince    string `json:"dateSince"`
	VersionTitle string `json:"versionTitle"`
	State        string `json:"state"`
}

type PricingDetails struct {
	Rate     string `json:"Rate"`
	Currency string `json:"Currency"`
	Unit     string `json:"Unit"`
}

type PricingInformation struct {
	Details []struct {
		OneMonths         PricingDetails `json:"1MONTHS"`
		TwelveMonths      PricingDetails `json:"12MONTHS"`
		DetailDescription string         `json:"DetailDescription"`
		Units             string         `json:"Units"`
		Name              string         `json:"Name"`
	} `json:"Details"`
}

type ProductData struct {
	ProductOverview    ProductOverview      `json:"ProductOverview"`
	SellerInformation  SellerInformation    `json:"SellerInformation"`
	ProductCode        string               `json:"ProductCode"`
	ProductType        string               `json:"ProductType"`
	ListingId          string               `json:"ListingId"`
	ProductId          string               `json:"ProductId"`
	VendorInsights     VendorInsights       `json:"VendorInsights"`
	VersionInformation []VersionInformation `json:"VersionInformation"`
	CustomerReviews    []interface{}        `json:"CustomerReviews"`
	OfferData          struct {
		PricingInformation PricingInformation `json:"PricingInformation"`
	} `json:"offerData"`
}

type DatadogProJSON struct {
	DoesProductSupportSubscriptionsFreeTrial bool        `json:"doesProductSupportSubscriptionsFreeTrial"`
	CanViewVendorInsightsComponents          bool        `json:"canViewVendorInsightsComponents"`
	SecurityInsightArn                       string      `json:"securityInsightArn"`
	CanViewProcurement                       bool        `json:"canViewProcurement"`
	PageTitle                                string      `json:"pageTitle"`
	CDNUrl                                   string      `json:"cdnUrl"`
	IsProductSunset                          bool        `json:"isProductSunset"`
	ProductData                              ProductData `json:"productData"`
	OfferData                                struct {
		PricingInformation PricingInformation `json:"PricingInformation"`
	} `json:"offerData"`
	SupportsContractFreeTrial     bool          `json:"SupportsContractFreeTrial"`
	ProductType                   string        `json:"ProductType"`
	ProductId                     string        `json:"ProductId"`
	OfferTags                     []interface{} `json:"OfferTags"`
	IsContractFreeTrialGA         bool          `json:"IsContractFreeTrialGA"`
	OfferId                       string        `json:"OfferId"`
	DiscoExceptionType            interface{}   `json:"DiscoExceptionType"`
	SupportsSubscriptionFreeTrial bool          `json:"SupportsSubscriptionFreeTrial"`
	EULALink                      string        `json:"EULALink"`
	UsageTermsUIFormatted         struct {
		UsageRatesUI []struct {
			Description string `json:"Description"`
			Price       struct {
				Rate     float64 `json:"Rate"`
				Currency string  `json:"Currency"`
				Unit     string  `json:"Unit"`
			} `json:"Price"`
			DisplayName interface{} `json:"DisplayName"`
			Name        string      `json:"Name"`
		} `json:"usageRatesUI"`
		UsageRates map[string]string `json:"usageRates"`
	} `json:"UsageTermsUIFormatted"`
	SupportInformation struct {
		AdditionalSupportResources []struct {
			Url      string `json:"Url"`
			LinkText string `json:"LinkText"`
		} `json:"AdditionalSupportResources"`
		RefundPolicy struct {
			EnFR interface{} `json:"en_FR"`
			EnUS string      `json:"en_US"`
		} `json:"RefundPolicy"`
		SupportResources interface{} `json:"SupportResources"`
		SupportDetails   string      `json:"SupportDetails"`
	} `json:"SupportInformation"`
}
