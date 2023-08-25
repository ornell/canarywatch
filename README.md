CanaryWatch - Monitoring connectivity issues inside your Kubernetes cluster.

Description:
------------
CanaryWatch is a tool designed to monitor and detect connectivity issues within a Kubernetes cluster. Deployed as a DaemonSet, each pod attempts to connect to every other pod within the cluster at specified intervals, reporting any anomalies or failures as Kubernetes events.

Features:
---------
1. Monitors pod-to-pod connectivity.
2. Logs connectivity issues and reports them as Kubernetes events.
3. Rate-limits event creation to avoid event spamming.
4. Adjusts check interval based on the size of the cluster.

Setup & Deployment:
-------------------
Please refer to the included Helm chart for deploying CanaryWatch in your Kubernetes cluster. Ensure you have the necessary permissions and RBAC roles set up for the application to function correctly.

Docker Image:
-------------
The CanaryWatch Docker image is available at gcr.io/ornell-211321/canarywatch/canarywatch:latest. Built for both amd64 and arm64 architectures.

Support & Issues:
-----------------
For support, issues, or feature requests, please file an issue on the project's GitHub page.

License:
--------
MIT

Credits:
--------
Developed by Thomas Ornell
