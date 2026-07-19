# Deploy GoGate to AWS (EKS + RDS + ElastiCache + ECR)
#
# Rough monthly cost while running (us-east-1, learning sizes):
#   EKS control plane ~$73
#   2x t3.medium      ~$60
#   NAT Gateway       ~$32 + data
#   RDS t3.micro      ~$15
#   Redis t3.micro    ~$12
# Tear down when idle: terraform destroy

## Prerequisites

```bash
export AWS_PROFILE=jigar
export AWS_REGION=us-east-1

terraform version
aws sts get-caller-identity
kubectl version --client
```

## 1. Terraform init + apply

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # edit if needed

terraform init
terraform plan
terraform apply
```

First apply can take **15–25 minutes** (EKS + RDS).

## 2. Wire kubectl

```bash
terraform output -raw configure_kubectl
# run the printed command, e.g.:
# aws eks update-kubeconfig --region us-east-1 --name gogate-dev --profile jigar

kubectl get nodes
```

## 3. Push image to ECR

```bash
ECR=$(terraform output -raw ecr_repository_url)
aws ecr get-login-password --region $AWS_REGION --profile $AWS_PROFILE \
  | docker login --username AWS --password-stdin ${ECR%%/*}

# from repo root (goget_gateway/)
docker build -t $ECR:latest .
docker push $ECR:latest
```

## 4. Create K8s secrets + deploy

```bash
cd ../k8s
cp secret.yaml.example secret.yaml
# fill DATABASE_URL / REDIS_URL from:
#   terraform output -raw rds_database_url
#   terraform output -raw redis_url
# also set JWT / master / provider keys

# set image in gateway.yaml to the ECR URL, then:
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f gateway.yaml

kubectl get pods,svc,hpa -n gogate
```

## 5. Destroy (stop the bill)

```bash
cd ../terraform
terraform destroy
```
