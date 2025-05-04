package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use: "oci-cli",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Debug: Executing command: %s\n", cmd.CommandPath())
		},
	}

	rootCmd.PersistentFlags().String("profile", "", "Specify the OCI config profile to use")

	var instancesCmd = &cobra.Command{
		Use:   "instances",
		Short: "Manage compute instances",
	}

	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List instances in a compartment or tenancy",
		Run: func(cmd *cobra.Command, args []string) {
			compartmentInput, _ := cmd.Flags().GetString("compartment-id")
			tenancyFlag, _ := cmd.Flags().GetString("tenancy")
			profileFlag, _ := cmd.Flags().GetString("profile")
			var configProvider common.ConfigurationProvider
			var err error

			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			var compartmentID string
			if tenancyFlag != "" {
				tenancyOCID, err := configProvider.TenancyOCID()
				if err != nil {
					log.Fatalf("Error getting tenancy OCID: %v", err)
				}
				compartmentID = tenancyOCID
			} else if compartmentInput != "" {
				compartmentID, err = resolveCompartmentID(compartmentInput, configProvider)
				if err != nil {
					log.Fatalf("Error resolving compartment: %v", err)
				}
			} else {
				tenancyOCID, err := configProvider.TenancyOCID()
				if err != nil {
					log.Fatalf("Error getting tenancy OCID for default: %v", err)
				}
				compartmentID = tenancyOCID
			}

			computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatalf("Error creating compute client: %v", err)
			}

			request := core.ListInstancesRequest{
				CompartmentId: &compartmentID,
			}
			response, err := computeClient.ListInstances(context.Background(), request)
			if err != nil {
				log.Fatal(err)
			}

			for _, instance := range response.Items {
				fmt.Printf("Instance ID: %s, Display Name: %s, State: %s\n", *instance.Id, *instance.DisplayName, instance.LifecycleState)
			}
		},
	}

	listCmd.Flags().String("compartment-id", "", "The OCID or friendly name of the compartment to list instances from")
	listCmd.Flags().String("tenancy", "", "Use the tenancy to list instances (ignores --compartment-id)")

	var createCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a new compute instance",
		Run: func(cmd *cobra.Command, args []string) {
			// 1. Get Flags
			profileFlag, _ := cmd.Flags().GetString("profile")
			nameFlag, _ := cmd.Flags().GetString("name")
			compartmentInput, _ := cmd.Flags().GetString("compartment-id")
			shapeNameFlag, _ := cmd.Flags().GetString("shape-name")
			imageNameFlag, _ := cmd.Flags().GetString("image-name")
			subnetIDFlag, _ := cmd.Flags().GetString("subnet-id")
			adFlag, _ := cmd.Flags().GetString("availability-domain")
			publicKeysFlag, _ := cmd.Flags().GetString("public-keys")
			ocpusFlag, _ := cmd.Flags().GetFloat32("ocpus")
			memoryInGBsFlag, _ := cmd.Flags().GetFloat32("memory-in-gbs")

			// 2. Setup Config Provider
			var configProvider common.ConfigurationProvider
			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			// 3. Create Compute Client
			computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatalf("Error creating compute client: %v", err)
			}

			// 4. Resolve Compartment ID
			var compartmentID string
			if compartmentInput != "" {
				compartmentID, err = resolveCompartmentID(compartmentInput, configProvider)
				if err != nil {
					log.Fatalf("Error resolving compartment ID '%s': %v", compartmentInput, err)
				}
			} else {
				compartmentID, err = configProvider.TenancyOCID()
				if err != nil {
					log.Fatalf("Error getting tenancy OCID: %v", err)
				}
			}
			fmt.Printf("Using Compartment ID: %s\n", compartmentID)

			// 5. Resolve Image ID
			tenancyOCID, err := configProvider.TenancyOCID()
			if err != nil {
				log.Fatalf("Error getting tenancy OCID: %v", err)
			}
			imageID, err := resolveImageNameToID(imageNameFlag, compartmentID, tenancyOCID, computeClient)
			if err != nil {
				log.Fatalf("Error resolving image name '%s': %v", imageNameFlag, err)
			}
			fmt.Printf("Using Image ID: %s\n", imageID)

			// 6. Validate Shape Name (resolveShapeNameToID currently validates existence)
			_, err = resolveShapeNameToID(shapeNameFlag, compartmentID, imageID, computeClient)
			if err != nil {
				log.Fatalf("Error validating shape name '%s' for image '%s': %v", shapeNameFlag, imageID, err)
			}
			fmt.Printf("Using Shape Name: %s\n", shapeNameFlag)

			// 7. Generate Display Name if needed
			displayName := nameFlag
			if displayName == "" {
				displayName = fmt.Sprintf("instance-%s", time.Now().Format("20060102-1504"))
			}
			fmt.Printf("Instance Display Name: %s\n", displayName)

			// 8. Prepare SSH Keys Metadata
			keys := strings.Split(publicKeysFlag, ",")
			sshKeysString := ""
			for i, key := range keys {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey != "" {
					sshKeysString += trimmedKey
					if i < len(keys)-1 {
						sshKeysString += "\n"
					}
				}
			}
			if sshKeysString == "" {
				log.Fatalf("Error: No valid public SSH keys provided.")
			}
			metadata := map[string]string{"ssh_authorized_keys": sshKeysString}

			// 9. Prepare VNIC Details
			createVnicDetails := core.CreateVnicDetails{
				SubnetId: &subnetIDFlag,
				// AssignPublicIp: common.Bool(true), // Default is usually true, explicitly set if needed
			}

			// 10. Prepare Source Details
			sourceDetails := core.InstanceSourceViaImageDetails{
				ImageId: &imageID,
			}

			// 11. Build Launch Instance Details
			launchDetails := core.LaunchInstanceDetails{
				AvailabilityDomain: &adFlag,
				CompartmentId:      &compartmentID,
				DisplayName:        &displayName,
				Shape:              &shapeNameFlag,
				CreateVnicDetails:  &createVnicDetails,
				SourceDetails:      sourceDetails,
				Metadata:           metadata,
			}

			// Add shape config for Flex shapes
			if ocpusFlag != 0 || memoryInGBsFlag != 0 {
				shapeConfig := core.LaunchInstanceShapeConfigDetails{
					Ocpus:        common.Float32(ocpusFlag),
					MemoryInGBs: common.Float32(memoryInGBsFlag),
				}
				launchDetails.ShapeConfig = &shapeConfig
			}

			// 12. Create Launch Request
			request := core.LaunchInstanceRequest{
				LaunchInstanceDetails: launchDetails,
			}

			fmt.Println("Launching instance...")

			// 13. Call API
			response, err := computeClient.LaunchInstance(context.Background(), request)
			if err != nil {
				log.Fatalf("Error launching instance: %v", err)
			}

			// 14. Print Result
			fmt.Printf("Instance launch initiated successfully.\nInstance ID: %s\nState: %s\n", *response.Instance.Id, response.Instance.LifecycleState)
			fmt.Println("Note: Instance provisioning takes time. Use 'instances info' to check status.")

		},
	}
	// Add flags needed for instance creation
	createCmd.Flags().String("name", "", "(Optional) Display name for the new instance (auto-generated if empty)")
	createCmd.Flags().String("compartment-id", "", "(Optional) OCID or name of the compartment to create instance in (defaults to tenancy root)")
	createCmd.Flags().String("shape-name", "", "Shape name for the new instance (e.g., VM.Standard.A1.Flex) (Required)")
	createCmd.Flags().String("image-name", "", "Display name of the OS image (e.g., 'Canonical Ubuntu 24.04 Minimal aarch64') (Required)")
	createCmd.Flags().String("subnet-id", "", "OCID of the subnet for the instance's VNIC (Required)")
	createCmd.Flags().String("availability-domain", "", "Availability Domain name (e.g., 'Uocm:US-ASHBURN-AD-1') (Required)")
	createCmd.Flags().String("public-keys", "", "Comma-separated list of public SSH keys (Required)")
	createCmd.Flags().Float32("ocpus", 0, "(Required for Flex shapes) Number of OCPUs")
	createCmd.Flags().Float32("memory-in-gbs", 0, "(Optional for Flex shapes) Amount of memory in GB")
	// Mark required flags
	_ = createCmd.MarkFlagRequired("shape-name")
	_ = createCmd.MarkFlagRequired("image-name")
	_ = createCmd.MarkFlagRequired("subnet-id")
	_ = createCmd.MarkFlagRequired("availability-domain")
	_ = createCmd.MarkFlagRequired("public-keys")

	var infoCmd = &cobra.Command{
		Use:   "info",
		Short: "Show information about a compute instance",
		PreRun: func(cmd *cobra.Command, args []string) {
			fmt.Println("Debug: About to run instances info command")
		},
		Run: func(cmd *cobra.Command, args []string) {
			idFlag, _ := cmd.Flags().GetString("id")
			nameFlag, _ := cmd.Flags().GetString("name")
			compartmentFlag, _ := cmd.Flags().GetString("compartment-id")
			profileFlag, _ := cmd.Flags().GetString("profile")
			var configProvider common.ConfigurationProvider
			var err error

			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			if idFlag != "" && nameFlag != "" {
				fmt.Println("Error: Specify either --id or --name, not both.")
				os.Exit(1)
			} else if idFlag != "" {
				computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
				if err != nil {
					fmt.Printf("Error: Creating compute client failed: %v\n", err)
					os.Exit(1)
				}
				request := core.GetInstanceRequest{InstanceId: &idFlag}
				response, err := computeClient.GetInstance(context.Background(), request)
				if err != nil {
					fmt.Printf("Error: Getting instance by ID failed: %v\n", err)
					os.Exit(1)
				}
				displayInstanceDetails(&response.Instance)
			} else if nameFlag != "" {
				var compartmentID string
				if compartmentFlag == "" {
					tenancyOCID, err := configProvider.TenancyOCID()
					if err != nil {
						fmt.Printf("Error: Getting tenancy OCID failed: %v\n", err)
						os.Exit(1)
					}
					compartmentID = tenancyOCID
				} else {
					compartmentID, err = resolveCompartmentID(compartmentFlag, configProvider)
					if err != nil {
						fmt.Printf("Error: Resolving compartment ID failed: %v\n", err)
						os.Exit(1)
					}
				}

				computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
				if err != nil {
					fmt.Printf("Error: Creating compute client failed: %v\n", err)
					os.Exit(1)
				}
				listRequest := core.ListInstancesRequest{CompartmentId: &compartmentID}
				listResponse, err := computeClient.ListInstances(context.Background(), listRequest)
				if err != nil {
					fmt.Printf("Error: Listing instances failed: %v\n", err)
					os.Exit(1)
				}
				found := false
				for _, instanceSummary := range listResponse.Items {
					if *instanceSummary.DisplayName == nameFlag {
						getRequest := core.GetInstanceRequest{InstanceId: instanceSummary.Id}
						fullResponse, err := computeClient.GetInstance(context.Background(), getRequest)
						if err != nil {
							fmt.Printf("Error: Getting full instance details failed: %v\n", err)
							os.Exit(1)
						}
						displayInstanceDetails(&fullResponse.Instance)
						found = true
						break
					}
				}
				if !found {
					fmt.Println("No instance found with that display name in the compartment.")
				}
			} else {
				fmt.Println("Error: Specify either --id or --name.")
				os.Exit(1)
			}
		},
	}

	infoCmd.Flags().String("id", "", "The OCID of the instance to get info for")
	infoCmd.Flags().String("name", "", "The display name of the instance to search for")
	infoCmd.Flags().String("compartment-id", "", "The OCID or friendly name of the compartment (optional, defaults to tenancy if not specified)")

	// Define list-images command
	var listImagesCmd = &cobra.Command{
		Use:   "list-images",
		Short: "List available compute images (custom or platform)",
		Run: func(cmd *cobra.Command, args []string) {
			// 1. Get Flags
			profileFlag, _ := cmd.Flags().GetString("profile")
			compartmentInput, _ := cmd.Flags().GetString("compartment-id")
			platformFlag, _ := cmd.Flags().GetBool("platform")
			osFilter, _ := cmd.Flags().GetString("os")
			limitFlag, _ := cmd.Flags().GetInt("limit")

			// 2. Setup Config Provider
			var configProvider common.ConfigurationProvider
			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			// 3. Create Compute Client
			computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatalf("Error creating compute client: %v", err)
			}

			// 4. Determine Compartment ID for Query
			tenancyOCID, err := configProvider.TenancyOCID()
			if err != nil {
				log.Fatalf("Error getting tenancy OCID: %v", err)
			}

			var queryCompartmentID string
			if platformFlag {
				// Platform images are typically queried against the tenancy OCID
				queryCompartmentID = tenancyOCID
				fmt.Println("Listing platform images...")
			} else if compartmentInput != "" {
				queryCompartmentID, err = resolveCompartmentID(compartmentInput, configProvider)
				if err != nil {
					log.Fatalf("Error resolving compartment ID '%s': %v", compartmentInput, err)
				}
				fmt.Printf("Listing images in compartment: %s\n", queryCompartmentID)
			} else {
				// Default to listing custom images in the tenancy root if no specific compartment or platform flag is given
				queryCompartmentID = tenancyOCID
				fmt.Printf("Listing images in tenancy root: %s\n", queryCompartmentID)
			}

			// 5. Build ListImages Request
			request := core.ListImagesRequest{
				CompartmentId: &queryCompartmentID,
				Limit:         common.Int(limitFlag),
				SortBy:        core.ListImagesSortByTimecreated,
				SortOrder:     core.ListImagesSortOrderDesc,
			}
			if osFilter != "" {
				request.OperatingSystem = &osFilter
			}

			fmt.Println("Fetching images...")

			// 6. Call API
			response, err := computeClient.ListImages(context.Background(), request)
			if err != nil {
				log.Fatalf("Error listing images: %v", err)
			}

			// 7. Print Results
			if len(response.Items) == 0 {
				fmt.Println("No images found matching the criteria.")
				return
			}

			fmt.Printf("Found %d images:\n", len(response.Items))
			fmt.Println("--------------------------------------------------")
			for _, image := range response.Items {
				fmt.Printf("Display Name: %s\n", *image.DisplayName)
				fmt.Printf("  ID:           %s\n", *image.Id)
				fmt.Printf("  OS:           %s\n", *image.OperatingSystem)
				if image.BaseImageId != nil {
					fmt.Printf("  Base Image:   %s\n", *image.BaseImageId)
				}
				fmt.Printf("  State:        %s\n", image.LifecycleState)
				fmt.Println("--------------------------------------------------")
			}
		},
	}

	// Add flags to list-images command
	listImagesCmd.Flags().String("compartment-id", "", "(Optional) OCID or name of the compartment to list custom images from (defaults to tenancy root)")
	listImagesCmd.Flags().Bool("platform", false, "List only platform images (ignores compartment-id)")
	listImagesCmd.Flags().String("os", "", "(Optional) Filter by operating system name (e.g., 'Oracle Linux', 'Ubuntu')")
	listImagesCmd.Flags().Int("limit", 50, "(Optional) Limit the number of results returned")

	// Define list-shapes command
	var listShapesCmd = &cobra.Command{
		Use:   "list-shapes",
		Short: "List available compute shapes for a compartment",
		Long:  `Lists compute shapes available in a specific compartment, optionally filtered by a specific image ID.`,
		Run: func(cmd *cobra.Command, args []string) {
			// 1. Get Flags
			profileFlag, _ := cmd.Flags().GetString("profile")
			compartmentInput, _ := cmd.Flags().GetString("compartment-id")
			imageIDFlag, _ := cmd.Flags().GetString("image-id")
			limitFlag, _ := cmd.Flags().GetInt("limit")

			// 2. Setup Config Provider
			var configProvider common.ConfigurationProvider
			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			// 3. Create Compute Client
			computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatalf("Error creating compute client: %v", err)
			}

			// 4. Resolve Compartment ID
			var compartmentID string
			if compartmentInput != "" {
				compartmentID, err = resolveCompartmentID(compartmentInput, configProvider)
				if err != nil {
					log.Fatalf("Error resolving compartment ID '%s': %v", compartmentInput, err)
				}
			} else {
				compartmentID, err = configProvider.TenancyOCID()
				if err != nil {
					log.Fatalf("Error getting tenancy OCID: %v", err)
				}
			}

			// 5. Build ListShapes Request
			request := core.ListShapesRequest{
				CompartmentId: &compartmentID,
				Limit:         common.Int(limitFlag),
			}
			if imageIDFlag != "" {
				request.ImageId = &imageIDFlag
			}

			fmt.Println("Fetching shapes...")

			// 6. Call API
			response, err := computeClient.ListShapes(context.Background(), request)
			if err != nil {
				log.Fatalf("Error listing shapes: %v", err)
			}

			// 7. Print Results
			if len(response.Items) == 0 {
				fmt.Println("No shapes found matching the criteria.")
				return
			}

			fmt.Printf("Found %d shapes:\n", len(response.Items))
			fmt.Println("--------------------------------------------------")
			for _, shape := range response.Items {
				fmt.Printf("Shape Name: %s\n", *shape.Shape)
				if shape.ProcessorDescription != nil {
					fmt.Printf("  Processor:  %s\n", *shape.ProcessorDescription)
				}
				if shape.OcpuOptions != nil {
					fmt.Printf("  OCPUs:      Min=%.2f, Max=%.2f\n", *shape.OcpuOptions.Min, *shape.OcpuOptions.Max) // Commenting out Default for now: , *shape.OcpuOptions.DefaultPerOcpu)
				}
				if shape.MemoryOptions != nil {
					fmt.Printf("  Memory (GB):Min=%.1f, Max=%.1f, Default=%.1f\n", *shape.MemoryOptions.MinInGBs, *shape.MemoryOptions.MaxInGBs, *shape.MemoryOptions.DefaultPerOcpuInGBs)
				}
				if shape.NetworkingBandwidthOptions != nil {
				    fmt.Printf("  Net BW(Gbps):Min=%.1f, Max=%.1f, Default=%.1f\n", *shape.NetworkingBandwidthOptions.MinInGbps, *shape.NetworkingBandwidthOptions.MaxInGbps, *shape.NetworkingBandwidthOptions.DefaultPerOcpuInGbps)
				}
				// Print other relevant fields if needed
				fmt.Println("--------------------------------------------------")
			}
		},
	}

	// Add flags to list-shapes command
	listShapesCmd.Flags().String("compartment-id", "", "(Optional) OCID or name of the compartment (defaults to tenancy root)")
	listShapesCmd.Flags().String("image-id", "", "(Optional) Filter shapes compatible with a specific image OCID")
	listShapesCmd.Flags().Int("limit", 100, "(Optional) Limit the number of results returned")

	instancesCmd.AddCommand(listCmd, createCmd, infoCmd, listImagesCmd, listShapesCmd)

	// --- Compartments Commands --- 
	var compartmentsCmd = &cobra.Command{
		Use:   "compartments",
		Short: "Manage compartments",
	}

	var listCompartmentsCmd = &cobra.Command{
		Use:   "list",
		Short: "List all compartments in the tenancy",
		Run: func(cmd *cobra.Command, args []string) {
			profileFlag, _ := cmd.Flags().GetString("profile")
			var configProvider common.ConfigurationProvider
			var err error

			if profileFlag != "" {
				configProvider = common.CustomProfileConfigProvider("~/.oci/config", profileFlag)
			} else {
				configProvider = common.DefaultConfigProvider()
			}

			tenancyOCID, err := configProvider.TenancyOCID()
			if err != nil {
				fmt.Printf("Error getting tenancy OCID: %v\n", err)
				os.Exit(1)
			}

			identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
			if err != nil {
				fmt.Printf("Error creating identity client: %v\n", err)
				os.Exit(1)
			}

			request := identity.ListCompartmentsRequest{
				CompartmentId: &tenancyOCID,
			}

			err = listCompartmentsRecursive(identityClient, &request, 0)
			if err != nil {
				log.Fatal(err)
			}
		},
	}

	compartmentsCmd.AddCommand(listCompartmentsCmd)

	rootCmd.AddCommand(instancesCmd, compartmentsCmd)

	rootCmd.Execute()
}

func resolveCompartmentID(input string, configProvider common.ConfigurationProvider) (string, error) {
	var err error
	// Check if the input is already an OCID
	if strings.HasPrefix(input, "ocid1.compartment.oc1.") || strings.HasPrefix(input, "ocid1.tenancy.oc1.") {
		return input, nil
	}

	// Input is likely a name, try to resolve it
	tenancyOCID, err := configProvider.TenancyOCID()
	if err != nil {
		return "", fmt.Errorf("failed to get tenancy OCID: %w", err)
	}

	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create identity client: %w", err)
	}

	request := identity.ListCompartmentsRequest{
		CompartmentId: &tenancyOCID,
	}
	response, err := identityClient.ListCompartments(context.Background(), request)
	if err != nil {
		return "", err
	}

	for _, compartment := range response.Items {
		if *compartment.Name == input {
			return *compartment.Id, nil
		}
	}

	return "", fmt.Errorf("compartment with name '%s' not found", input)
}

func listCompartmentsRecursive(client identity.IdentityClient, request *identity.ListCompartmentsRequest, depth int) error {
	var err error
	response, err := client.ListCompartments(context.Background(), *request)
	if err != nil {
		return err
	}

	for _, compartment := range response.Items {
		indent := strings.Repeat("  ", depth)
		fmt.Printf("%sCompartment ID: %s, Name: %s, Description: %s\n", indent, *compartment.Id, *compartment.Name, *compartment.Description)

		// Recurse into sub-compartments if any exist
		if compartment.Id != nil {
			subRequest := identity.ListCompartmentsRequest{
				CompartmentId: compartment.Id,
			}
			err = listCompartmentsRecursive(client, &subRequest, depth+1)
			if err != nil {
				return err
			}
		}
	}

	// Handle pagination if needed (e.g., if there's a next page token)
	if response.OpcNextPage != nil {
		nextRequest := *request
		nextRequest.Page = response.OpcNextPage
		return listCompartmentsRecursive(client, &nextRequest, depth)
	}

	return nil
}

func displayInstanceDetails(instance *core.Instance) {
	fmt.Println("Instance Details:")
	fmt.Printf("  ID: %s\n", *instance.Id)
	fmt.Printf("  Display Name: %s\n", *instance.DisplayName)
	fmt.Printf("  State: %s\n", instance.LifecycleState)
	fmt.Printf("  Shape: %s\n", *instance.Shape)
	fmt.Printf("  Image ID: %s\n", *instance.ImageId)
	fmt.Printf("  Compartment ID: %s\n", *instance.CompartmentId)
	fmt.Printf("  Availability Domain: %s\n", *instance.AvailabilityDomain)
	fmt.Printf("  Fault Domain: %s\n", *instance.FaultDomain)
}

// resolveImageNameToID finds the OCID for a given image display name.
// It needs the tenancyOCID for fallback searches for platform images.
func resolveImageNameToID(imageName, compartmentID, tenancyOCID string, client core.ComputeClient) (string, error) {
	request := core.ListImagesRequest{
		CompartmentId: &compartmentID,
		DisplayName:   &imageName,
		// Add other filters if needed, e.g., OperatingSystem
	}
	response, err := client.ListImages(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to list images: %w", err)
	}

	if len(response.Items) == 0 {
		// Try searching using the tenancy OCID (common practice for platform images)
		fmt.Printf("Image '%s' not found in compartment '%s', checking platform images...\n", imageName, compartmentID)
		request.CompartmentId = &tenancyOCID // Use Tenancy OCID for fallback
		responseOracle, errOracle := client.ListImages(context.Background(), request)
		if errOracle != nil {
			// Provide more context in the error
			return "", fmt.Errorf("failed to list platform images (using tenancy %s): %w", tenancyOCID, errOracle)
		}
		if len(responseOracle.Items) == 0 {
			return "", fmt.Errorf("no image found with name '%s' in compartment '%s' or platform images (searched tenancy %s)", imageName, compartmentID, tenancyOCID)
		}
		if len(responseOracle.Items) > 1 {
			fmt.Printf("Warning: Multiple platform images found with name '%s'. Using the first one.\n", imageName)
		}
		return *responseOracle.Items[0].Id, nil
	}

	if len(response.Items) > 1 {
		fmt.Printf("Warning: Multiple images found with name '%s' in compartment '%s'. Using the first one.\n", imageName, compartmentID)
	}

	return *response.Items[0].Id, nil
}

// resolveShapeNameToID finds the OCID for a given shape name.
// Note: Shape OCIDs are usually not required, the name often suffices, but this provides flexibility.
func resolveShapeNameToID(shapeName string, compartmentID string, imageID string, client core.ComputeClient) (string, error) {
	request := core.ListShapesRequest{
		CompartmentId: &compartmentID,
		ImageId:       &imageID, // Shapes depend on the image
	}
	response, err := client.ListShapes(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to list shapes: %w", err)
	}

	for _, shape := range response.Items {
		if shape.Shape != nil && *shape.Shape == shapeName {
			// The SDK often uses the shape *name* directly, but we found it.
			// If the API truly needed the shape OCID (uncommon), we'd return it here.
			// For now, we just confirm it exists and return the name as the API expects.
			return shapeName, nil
		}
	}

	return "", fmt.Errorf("no shape found with name '%s' compatible with image '%s' in compartment '%s'", shapeName, imageID, compartmentID)
}
