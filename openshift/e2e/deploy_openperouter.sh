set -euo pipefail

# Create OpenPERouter CR
cat <<'EOF' | oc apply -f -
apiVersion: network.openperouter.io/v1alpha1
kind: OpenPERouter
metadata:
  name: openperouter
  namespace: openshift-openperouter
spec:
  logLevel: info
EOF

# Wait for controller and router daemonsets to be created and rolled out
for ds in controller router; do
  echo "Waiting for daemonset $ds to be created..."
  oc wait --for=create daemonset/"$ds" -n openshift-openperouter --timeout=300s
  oc rollout status daemonset/"$ds" -n openshift-openperouter --timeout=300s
done

echo "=== Deploy verification ==="
oc get pods -n openshift-openperouter -o wide
oc get daemonset -n openshift-openperouter

# Verify all pods are Running and Ready
NOT_READY=$(oc get pods -n openshift-openperouter --no-headers | grep -v "Completed" | grep -v "1/1\|2/2\|3/3\|4/4\|5/5" || true)
if [ -n "$NOT_READY" ]; then
  echo "ERROR: Some pods are not fully ready:"
  echo "$NOT_READY"
  exit 1
fi

echo "All openperouter pods are running and ready"
