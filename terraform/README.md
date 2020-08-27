# Using Terraform to run Fleetspeak End-to-End testing in Google Cloud

## Installing Terraform

Please follow [these instructions](https://www.terraform.io/intro/getting-started/install.html) to install Terraform binary on your machine.

## Setting up a Google Cloud Project

1.  Create a new project in Google Cloud Platform console ([link](https://console.cloud.google.com/project)).
1.  Enable billing for the project ([link](https://support.google.com/cloud/answer/6293499#enable-billing)).
1.  Enable Compute Engine and Cloud SQL APIs ([link](https://console.cloud.google.com/flows/enableapi?apiid=compute_component,sqladmin)).

## Instrumenting Terraform with credentials

1.  Install Google Cloud SDK
    ([link](https://cloud.google.com/sdk/install)).
2.  Obtain Google Cloud credentials by running the following:
```bash
gcloud auth application-default login
```
Your credentials will be saved to a file `~/.config/gcloud/application_default_credentials.json`. It will be used by terraform.

## Running Terraform
1.  `cd` to terraform directory
2.  Run the following to initialize Terraform:
```bash
terraform init
```
3.  Then run the following to start installation:
```bash
terraform apply -var "project_name=*your_project_name*"
```

By default 1 fleetspeak server and 1 client will be started. You can specify these numbers with terraform variables:
```bash
terraform apply -var "project_name=*your_project_name*" -var "num_servers=2" -var "num_clients=3"
```

If you run the installation in the internal Google Cloud, the default image of virtual machines may be not allowed. In this case run `terraform apply` with the additional variable as following:
```bash
terraform apply -var "project_name=*your_project_name*" -var "vm_image=projects/eip-images/global/images/ubuntu-1804-lts-drawfork-v20200208"
```

## Viewing tests results
After all the tests are completed, the `results.txt` file will be uploaded into the bucket created by Terraform ([link](https://console.cloud.google.com/storage)).

## Destroying resources
When tests are finished, destroy all the resources by running
```bash
terraform destroy -var "project_name=*your_project_name*"
```
