terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Local state for learning. Production teams use an S3 backend + DynamoDB lock:
  # backend "s3" {
  #   bucket         = "gogate-terraform-state"
  #   key            = "prod/terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "gogate-terraform-locks"
  #   encrypt        = true
  # }
}