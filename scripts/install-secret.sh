#!/bin/bash

# Check if the required arguments are provided
if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <org-name> <org-key>"
  exit 1
fi

# Assign positional parameters to variables
ORG_NAME=$1
ORG_KEY=$2

# Create the Kubernetes secret in the controlplane namespace
kubectl create secret generic "$ORG_NAME" \
  --from-literal=token="$ORG_KEY" \
  --namespace=controlplane

# Add the label to the secret
kubectl label secret "$ORG_NAME" \
  app.kubernetes.io/managed-by=cpln-operator \
  --namespace=controlplane

# Check if the secret creation and labeling were successful
if [ $? -eq 0 ]; then
  echo "Secret for organization '$ORG_NAME' created and labeled successfully in the 'controlplane' namespace."
else
  echo "Failed to create or label secret for organization '$ORG_NAME'."
  exit 1
fi