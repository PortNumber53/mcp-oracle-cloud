package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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

	var instancesCmd = &cobra.Command{
		Use:   "instances",
		Short: "Manage compute instances",
	}

	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List compute instances",
		Run: func(cmd *cobra.Command, args []string) {
			configProvider := common.DefaultConfigProvider()
			tenancyFlag, _ := cmd.Flags().GetString("tenancy")
			var compartmentID string
			var err error

			if tenancyFlag != "" {
				compartmentID, err = configProvider.TenancyOCID()
				if err != nil {
					log.Fatal(err)
				}
			} else {
				compartmentInput, _ := cmd.Flags().GetString("compartment-id")
				if compartmentInput == "" {
					fmt.Println("Compartment ID, name, or --tenancy is required")
					os.Exit(1)
				}
				compartmentID, err = resolveCompartmentID(compartmentInput)
				if err != nil {
					log.Fatal(err)
				}
			}

			computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatal(err)
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
			fmt.Println("Create command not implemented yet")
		},
	}

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
			fmt.Printf("Debug: Flags retrieved - id='%s', name='%s', compartment-id='%s'\n", idFlag, nameFlag, compartmentFlag)
			configProvider := common.DefaultConfigProvider()
			fmt.Printf("Debug: Info command started with flags: id='%s', name='%s', compartment-id='%s'\n", idFlag, nameFlag, compartmentFlag)

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
					compartmentIDResolved, err := resolveCompartmentID(compartmentFlag)
					if err != nil {
						fmt.Printf("Error: Resolving compartment ID failed: %v\n", err)
						os.Exit(1)
					}
					compartmentID = compartmentIDResolved
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
						break // Assuming names are unique, stop after first match
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

	instancesCmd.AddCommand(listCmd, createCmd, infoCmd)

	var compartmentsCmd = &cobra.Command{
		Use:   "compartments",
		Short: "Manage compartments",
	}

	var listCompartmentsCmd = &cobra.Command{
		Use:   "list",
		Short: "List all compartments",
		Run: func(cmd *cobra.Command, args []string) {
			configProvider := common.DefaultConfigProvider()
			identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
			if err != nil {
				log.Fatal(err)
			}

			// Get tenancy OCID with error handling
			tenancyOCID, err := configProvider.TenancyOCID()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Tenancy ID: %s\n", tenancyOCID)
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

func resolveCompartmentID(input string) (string, error) {
	configProvider := common.DefaultConfigProvider()
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
	if err != nil {
		return "", err
	}

	// Check if input looks like an OCID (starts with 'ocid1.')
	if strings.HasPrefix(input, "ocid1.") {
		return input, nil // Assume it's a valid OCID
	}

	// Get tenancy OCID, handling error
	tenancyOCID, err := configProvider.TenancyOCID()
	if err != nil {
		return "", err
	}

	// Treat input as a compartment name and query Identity service
	request := identity.ListCompartmentsRequest{
		CompartmentId: &tenancyOCID,
	}
	response, err := identityClient.ListCompartments(context.Background(), request)
	if err != nil {
		return "", err
	}

	for _, compartment := range response.Items {
		if *compartment.Name == input {
			return *compartment.Id, nil // Return the OCID of the matching compartment
		}
	}

	return "", fmt.Errorf("compartment with name '%s' not found", input)
}

func listCompartmentsRecursive(client identity.IdentityClient, request *identity.ListCompartmentsRequest, depth int) error {
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
		nextRequest := *request // Copy the request
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
	fmt.Printf("  Fault Domain: %s\n", *instance.FaultDomain) // Kept if valid, but can be removed if not needed
	// Removed SubnetId as it was undefined; consider adding VNIC details if required in future updates.
}
