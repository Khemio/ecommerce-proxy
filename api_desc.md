Common Product Fields Reference
Here are the most frequently used fields you will likely need for your Go structs:

Field	Type	Description
id	ID	The Global ID (required for updates).
title	String	The product name.
handle	String	URL-friendly slug (e.g., red-snowboard).
description	String	The HTML description.
productType	String	Category (e.g., "Snowboard").
vendor	String	The manufacturer/brand.
status	Enum	ACTIVE, DRAFT, ARCHIVED.
variants	Connection	List of variants (sizes, colors).
images	Connection	List of product images.
metafields	Connection	Custom data fields.
createdAt	DateTime	Creation timestamp.
updatedAt	DateTime	Last modified timestamp.
