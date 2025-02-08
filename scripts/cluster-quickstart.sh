kind create cluster --name operator

#cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml

#Argo
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# Wait for cert-manager webhook to be ready
while ! kubectl get pods -n cert-manager | grep -q "webhook.*1/1.*Running"; do
  echo "Waiting for cert-manager webhook to be ready..."
  sleep 5
done
echo "cert-manager webhook is ready."

#cpln-operator
helm repo add cpln https://controlplane-com.github.io/k8s-operator
helm repo update cpln
helm install cpln-operator cpln/cpln-operator