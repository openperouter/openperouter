# Implementation Validation

**Baseline revision**: `a8ecb2764e5f4df9899935465b826114ed96d85a`
**Started**: 2026-10-28
**Environment**: Go 1.26.4; Linux 6.19.10-300.fc44.x86_64; Docker available; no containers running at baseline.
**Working tree**: No tracked changes at baseline. Existing untracked skills, .specify, .gocache, nocontainer.md and specs preserved.
**Specification checklist**: 16/16 passing. No implementation hooks configured.

## Public contract and e2e baseline fingerprints

```text
1f69da1631c812edefb77607c15c0b1feb09a14e880eb311a8ca1bb8d2ab0ee8  .github/workflows/ci.yaml
e35c1d3f702d8bf5a7d6f2aa4597ef328e5e0d6f25e518aa1aee2af813b5e5a6  api/static/config.go
2641281342c670d05a61d1d4918092fc0b2af9801e2d61d771f18de3c6f3d4ae  api/v1alpha1/failedresource.go
f78562595704ac524cc4d111306cefb9f4b86f6fe0ec69425327b8ea306e6237  api/v1alpha1/groupversion_info.go
452c218833a495e49cec3c1d17f9aef9822a7d388b92691976ecdb084ca860b8  api/v1alpha1/hostsession.go
46a2969327e94e95936d17a14a8b4824e33b37f3d85a9f12c01880a7c839bcdf  api/v1alpha1/l2vni_types.go
decb2381b365fb5182680c613dee0e2c9f50518b3917302fd314d81f7ae915ca  api/v1alpha1/l3passthrough_types.go
53f147a35867026f77822eb7aafe2a8ca6c1b8ca98cebd2a261a98405f38d056  api/v1alpha1/l3vni_types.go
3f7ccce82a49d956934565fde352f892036bf9adaacc3e6bd12b4f0e9885bf65  api/v1alpha1/l3vpn_types.go
d0c6188dbf0dfb00ddf4a30bb3e617ca68dd0705895439d348fc9c00286e02b1  api/v1alpha1/neighbor.go
56130273cc9f3bc8e20e3ad390f874d11681fc23f8cf3a9f55a568a6b81bd3cf  api/v1alpha1/rawfrrconfig_types.go
f27868fea77c465d30cf8899db7d55a30a2e006fe1b95fc573a64b8aee2b846b  api/v1alpha1/routernodeconfigurationstatus_types.go
a6d7fb5f1e73c3df6a80091c16a8d6c2b58179926651c4c58f31b757ea09b71f  api/v1alpha1/underlay_types.go
976ae85c193258f792f0842aa141386a71993d9bb62369735a045a2a75c2c319  api/v1alpha1/zz_generated.deepcopy.go
bb987e6a8ef45a99255d1d632812482d5f0f3255ea841520b4d90342df5fe2b3  charts/openperouter/.helmignore
a4cc31089b29b1f7ea243d1277d8ba68d2a597720f411e62e64e9a474e8b49fa  charts/openperouter/Chart.lock
bc950b777d39a979fa30ef09c6b6ef8fa160eb64c0bd2327d794e086207e6844  charts/openperouter/Chart.yaml
adcd242eb6b53eea3d8ae13a99b737e89151b87dabb38dedbe7ed7aac123fc0f  charts/openperouter/README.md
bb987e6a8ef45a99255d1d632812482d5f0f3255ea841520b4d90342df5fe2b3  charts/openperouter/charts/crds/.helmignore
a20db81df24862bdca58ed26f99a28effe4d1e5da0a91ef64077e9743994c162  charts/openperouter/charts/crds/Chart.yaml
1b03cc5506f5eba1da498d39deff48821957ca44e482e0673de46c273e473cba  charts/openperouter/charts/crds/templates/network.openperouter.io_l2vnis.yaml
c9881d18dc6b312c80a14bbb2bda818015b8b7cbe8f02fbfb4fa9f8a787737b6  charts/openperouter/charts/crds/templates/network.openperouter.io_l3passthroughs.yaml
e61ad406e2f37e40aac6096ace88bfe41024abf509059d6da0ea0126d1a56550  charts/openperouter/charts/crds/templates/network.openperouter.io_l3vnis.yaml
6677172f95eccac7424ce1b408a713628c893affbc20d916942570fc7eaa6a74  charts/openperouter/charts/crds/templates/network.openperouter.io_l3vpns.yaml
9766c7e6c47528d72d35040553d90120b38553efcf0c9a91327a7e55e8e1997d  charts/openperouter/charts/crds/templates/network.openperouter.io_rawfrrconfigs.yaml
0a64c6fb8d67de0ebcf354caf79f8aeffad0aff1369d94be212e2a2a3106937f  charts/openperouter/charts/crds/templates/network.openperouter.io_routernodeconfigurationstatuses.yaml
380f064392507e86ca5877613693d020f390b3d87af6fa93f59db1bdbcd57862  charts/openperouter/charts/crds/templates/network.openperouter.io_underlays.yaml
be1d436f1f9a4d4265568e1a6e2bff621703efa69d583c7dbb63de51daa05e5c  charts/openperouter/templates/NOTES.txt
f0397f3351c6ecdf859057d2253b01b426e8f03b13e14fc52be0ddd30444995e  charts/openperouter/templates/_helpers.tpl
3d4ee6e2b93ab181a36832a2c520d99d7b281bc73fbc826e41f6148217fd65cf  charts/openperouter/templates/controller.yaml
7c271d949c8add213291b59f6f18a45e7a588c72e7cc601899930d9d1604d7d8  charts/openperouter/templates/hostbridge.yaml
3cd6f90693e167d846905d12e891c14bb110fe6c319eaea30dc610d4382dc3d5  charts/openperouter/templates/nodemarker.yaml
68c5a1b27a399851bb550ec1ed6a08f7670e81e4371a2f320402be7a7d02bfa8  charts/openperouter/templates/rbac.yaml
8626212a3af1eff2d13b71ab96e07ed7cc71d1074841be3d5cda5d1d3e5ebbb6  charts/openperouter/templates/router.yaml
5b1265168ac3f7800e6feadd1c2049e3d5c854370afbf855d3db1191c00b6716  charts/openperouter/templates/service-accounts.yaml
dfcbade1e356bb0d6012b114075eb8130bc9f584321bbbc461391e86f11c38b0  charts/openperouter/templates/webhook.yaml
831a50b08ad0056802b100d7d776c9d6701914ce6a46af9ef88a2e0c42f0353b  charts/openperouter/values.schema.json
2ffc722a6d3cf9ad1ca022670e05486d90f1618af04afcbfbf819a941be99ddd  charts/openperouter/values.yaml
1b03cc5506f5eba1da498d39deff48821957ca44e482e0673de46c273e473cba  config/crd/bases/network.openperouter.io_l2vnis.yaml
c9881d18dc6b312c80a14bbb2bda818015b8b7cbe8f02fbfb4fa9f8a787737b6  config/crd/bases/network.openperouter.io_l3passthroughs.yaml
e61ad406e2f37e40aac6096ace88bfe41024abf509059d6da0ea0126d1a56550  config/crd/bases/network.openperouter.io_l3vnis.yaml
6677172f95eccac7424ce1b408a713628c893affbc20d916942570fc7eaa6a74  config/crd/bases/network.openperouter.io_l3vpns.yaml
9766c7e6c47528d72d35040553d90120b38553efcf0c9a91327a7e55e8e1997d  config/crd/bases/network.openperouter.io_rawfrrconfigs.yaml
0a64c6fb8d67de0ebcf354caf79f8aeffad0aff1369d94be212e2a2a3106937f  config/crd/bases/network.openperouter.io_routernodeconfigurationstatuses.yaml
380f064392507e86ca5877613693d020f390b3d87af6fa93f59db1bdbcd57862  config/crd/bases/network.openperouter.io_underlays.yaml
da351862dd4560bc7bcca6bd2cdf0ce8742de6852ec9bf45374d05a70f8856ac  e2etests/go.mod
f46aa3374a05dc33fb32619730dd5b2f13e4c0d9533f25bdda0f04a4985a2268  e2etests/go.sum
b3b8ce006edc5d4acd99717a1b592d813268985376579eebea9d95b390b5cb1e  e2etests/hw_emulation_suite/hw_emulation_common.go
00eb7375b441eb6615aa168ba5b95d48adb427049b470e3951dbab8347e76dd8  e2etests/hw_emulation_suite/hw_emulation_scenarios.go
dd8e59b9c43d5be45d45cc457cf0484525b99adbf9e5febfa91dbbd45dff175a  e2etests/hw_emulation_suite/suite_test.go
bdaf145c363566168398bd6e48fd1d8e2dc4b33c1c809af41dce7f1318ae54ab  e2etests/nodelink-default.json
5ffea7baf65b91ff41ae0273f9805eb514299d40532499f5053ba6d564a39fe8  e2etests/pkg/config/update.go
01d01574234e20631f7dd9889fe1944ad6cfcda77cc5ec88eaad6fe4cc062499  e2etests/pkg/executor/executor.go
eb507b97b8f1d75975193ea6af66a8305a4e3c32b54267eb6a456b321ae2d338  e2etests/pkg/executor/nodeexec.go
dcf3799326f7ff791ef26dc5096f09a4e4ba789f61b4b025aa46c0a5194cdbc6  e2etests/pkg/frr/bfd_test.go
ffe9a01106d9b1f0874acd6720e06c32838a7fd9a881f107ebaddd66e387bfaf  e2etests/pkg/frr/bgp.go
3c932712f425f92dbb695b0139fe34a9e161c75899f6d273f06178f0192de96a  e2etests/pkg/frr/container.go
075a658390689df01e80cd967be7c9a3b62aca88057f4e403ac8130cb0d79274  e2etests/pkg/frr/dump.go
979933704cff814aa0c63142bdb8a718f1cadb4010329680786e900f68631707  e2etests/pkg/frr/evpn.go
01a101c52cca10ddbb40903a7033e72b9197be0877b0b2c9230f8d79abaaa94e  e2etests/pkg/frr/evpn_test.go
6c7213cefeac8574a122ede001e2c0b4361b2ce6e9d2b756e71ee7e7a3ea57ab  e2etests/pkg/frr/grout_dump.go
9357711b35d75b2c7a1cc5fdff1806d4611d9b0563d5da6767d1bf71bb2fa8d9  e2etests/pkg/frr/isis.go
b7aa5bd410702f7f0b104e38cfaecc20ac3edca206031628b7402f44d1fad1bc  e2etests/pkg/frr/l3vpn.go
a34544d975ce2ee53a3d31be58274d79518b893afb39f617be88f1b7be74c466  e2etests/pkg/frr/routes.go
18f2cd9b7708af392799454f5f65652ad9e4c6500ee41a198cd451c12d93a82c  e2etests/pkg/frrk8s/config.go
61b08f1e2c543605bd6e984bb336134a83a205f70075a75c38c35a5c70982563  e2etests/pkg/infra/config.go
2b4020b9a6bde22cf526b3b7d3850bbf92ca06368870e7c53e9aff92d28f33bc  e2etests/pkg/infra/leaf.go
54d21270c230e36bf2e9b695d77ada764a3e761674177853e394ea4bda0f7efb  e2etests/pkg/infra/routers.go
f61104ccdd0ede8531fae4e347bbe9579c35726311160655f78d4a8e68e8b61e  e2etests/pkg/infra/testdata/leaf.srv6.tmpl
ad8c978346aebbb627965a8aa1bdd6785b0ea5a029e75756d218b2b7c9174a58  e2etests/pkg/infra/testdata/leaf.tmpl
2ac15bd676083337b8ba6213f55d145f14b21dee9b47459198e49bd382022752  e2etests/pkg/infra/testdata/leafkind.tmpl
625d221c6664df1ab771080926c222d5c6f60cebcddd587de9605a876c868da9  e2etests/pkg/infra/underlay.go
687021a1b7d5b626c8fc6d0cea88dfe2ff3d0371c8acbbb1aac181b2f049fb56  e2etests/pkg/infra/underlay_cni.go
3ae206d2c28645e71563a2ef96b44066352d5274e3e7f40a6486c4c7ba4b7cfb  e2etests/pkg/ipfamily/ipfamily.go
27322b6c46b2a989dd5a8e78ad0c5fc87736897f0cdb4e709b471b3bbe9f88ba  e2etests/pkg/ipfamily/ipfamily_test.go
c774001e552b6891dbe1114e1660b3cd442e055f589ae83cf55c048a89f72b23  e2etests/pkg/k8s/configmap.go
08bec06cfb41fb81e7bdb3e99c32a2f46a55087b3e2ab10eca0c42a38ff8fe8f  e2etests/pkg/k8s/daemonset.go
02ddc92a7e460ba91fa7aff70dacbd2c2ef6e580d58edee25d95d4eec4393670  e2etests/pkg/k8s/nad.go
592fd362fd61074abf981e651024b387dd595cea65d080915bec2d5d4f64aeea  e2etests/pkg/k8s/namespace.go
4e9ad594f0011941a5a5075b25a65e7476e038650e4df5bc2820afa757fb59cc  e2etests/pkg/k8s/nodes.go
aa142318bd1542b6ec102cf1888e4df29c59941635a1e3b8563e44a50898f721  e2etests/pkg/k8s/pods.go
9f0642a8bf67e3ca982ab0b41600e1fea8ef8491902b786c409b135897fff45d  e2etests/pkg/k8s/reporter.go
e6c93a6a98d2d9aca8ed3972dbac8422d766bc08b924bc61d5f229ed409b4515  e2etests/pkg/k8s/secret.go
7b8739c122089757585583c463583753a7334a4207f911f7861a97b9ff48372f  e2etests/pkg/k8s/services.go
7585d0aa660d289c53ec2f870f85694fe9013b3041c41af66e3d2408fcf74ef1  e2etests/pkg/k8sclient/client.go
29307628e62aad52a48cbdc7147b20316baa0dcbc690db101ee705a690b6f529  e2etests/pkg/metrics/metrics.go
7a4bef20914866d53d26a8940659d6c796823ea583e15ed362be3fe831595630  e2etests/pkg/metrics/metrics_test.go
04ca31540b75bd7524b02b53898bc5f5b840ff2c094dc0372a7b20326b43e13e  e2etests/pkg/networklayerprotocol/networklayerprotocol.go
de0cf050b2230eea02fcec8840acd63709bb874b51c15cb05835a8b3a4f6dd6c  e2etests/pkg/openperouter/cidr.go
a4b05f09e41668cd2a431bc443e832f58e871dfe186d00c919a8bb8780a58d41  e2etests/pkg/openperouter/controller.go
fe0808d4a46d355c3d0dab244cb707b4c6d4085d3eaab728040d843059f990a4  e2etests/pkg/openperouter/dump_podman.go
5c86cef5804d7e0140035627d1085d29990f11e4ae75a1e5e92923d5970ef49e  e2etests/pkg/openperouter/interface.go
3d86ee72aa45962b8da419c708a49dd83929f93d00e5a38af5cec2d3910fcd43  e2etests/pkg/openperouter/netns.go
9889d646042e2802b9335e198aaa686d541c4e01915805edcbbe02c997785850  e2etests/pkg/openperouter/openperouter.go
9c2768095d2e49018909e97d9df922e77a8109e80eaf52b38ec703da028ce87f  e2etests/pkg/openperouter/pod_routers.go
26165259ee9d1fe3007c6b2485838c5f41a1e290612b68332cdcd6ddcd4310f3  e2etests/pkg/openperouter/podman_routers.go
a56cc3d8a50692724af2aa80aea35e6caef8ca757dfd7dcc5ae15f08234a7521  e2etests/pkg/openperouter/status.go
929473af2024a1136b71a6e72d699dd2180ecdfa31e23df0ad38dbdf3249279f  e2etests/pkg/openperouter/vtep.go
7f6b14d733108a640420bb64eee3741d2cddc31acc56cba0b7cf59de359ddcc3  e2etests/pkg/openperouter/webhooks.go
abbbaf92b0cb444d6eada47c8711d49e7a2e9042ce41b194fd49f8bdf266ca75  e2etests/pkg/sysctl/sysctl.go
76761dc8759e7b7f671e9bbeafd4bfdb05f5e768b390057e94ffc7bb920f59ca  e2etests/pkg/systemd/units.go
85fa79d3be033a3f84a6fa167e7f4340f3916af4113a1992715bbad6a6093bad  e2etests/pkg/triage/dump.go
ee8838e1dd9767d337015f06d81f16c13149753bbe1299592baf260209a7764c  e2etests/pkg/url/url.go
579db315cf7bd985651272ceaee8cebd406e37c70f551e944872202e29cde583  e2etests/pkg/url/url_test.go
c3edc8a481bf1a6e3d0dc6dbdcd850b88895a4713f1f7d7aef5821d3a47c5ca6  e2etests/pkg/validate/validate_sessions.go
814c5dd153a274ad2e13f9b31f8c7bb959d62f81ee4223bb4cd1bd2e42a53d60  e2etests/scale_suite/suite_test.go
cfed06be95bbbdf867b6bfe793bdbe2090dde3e9f73acaa68ee2108199e0bfa4  e2etests/scale_tests/common.go
be23306ed4e262918ce47f8b850b2261eedf4d7019cc045bae73530827ed030b  e2etests/scale_tests/scale.go
cfcafa74797eefa858f75668ba169aa90243b83cc4494c95bb1069567375d829  e2etests/suite/suite_test.go
4a24132e02a4e4295ec0ae2b228772f6bf93da8571316807501d6f5f8093db86  e2etests/systemd_static_suite/suite_test.go
0485663810bb16ae331b9b8820c0052beda5dfbb3c7ec3ec741f771096b1ae15  e2etests/systemd_static_suite/systemd_static_files_test.go
52283be5d696cf0b2a5d3db3d34ba291435a97a34b7a0ce81963329051add943  e2etests/systemd_static_suite/testdata/openpe_underlay.yaml
d15bfd4f7b17c9f8a9cbcee102a17b50883c75ff84c65cccdfbcecd094aa5b82  e2etests/systemd_static_suite/testdata/openpe_vni.yaml
235a72891a70479d39f307a08078cf0d0d15a85c4cc67f00d6a1350b26027f1b  e2etests/systemd_static_suite/validate_routes.go
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  e2etests/test_plan.txt
b8e716be4d749293b53099bea801e3f0ff0066ac5176f1135279832191562380  e2etests/tests/bridgerefresh.go
ea22df20122843d62a48035b3b04da3e437b9b1bb7dd374234e2c9533629550b  e2etests/tests/cni_lifecycle.go
f33aac5f558392d19f444c4e72a01a84e87286cb6ed45f121cdada5408e1ac56  e2etests/tests/common.go
f2e62b5f5a6a3290fe6eeee7334a3d0131c2ec30115060981a18b408edffca87  e2etests/tests/evpn_l2.go
b2ec7958f921992c5b62a2f06ca797e29bf1ef6c80510d5dca0f0dc014e663bb  e2etests/tests/evpn_l2vnis_with_same_l3vni.go
9a15e9e012cca0339094752fce758241d778af37112de34d7db1745391140bd1  e2etests/tests/evpn_routes.go
1c49f99201bebab6dcc90cbb0c0e1e4d8e3c9d0a881b0f183feadf4251ef46e5  e2etests/tests/frr_daemon_death.go
c2ad69cf91e7430560104ec6ddef7bd79b07f3dff771948c8d7c4f857e95bdc5  e2etests/tests/frr_restart.go
0bcdb9b8b4c6897eb88bad8dd93a082f5ac741bc4714ca8e952b295d8d07ae59  e2etests/tests/ipv6_vtep.go
f97eff8d89dac71cadd3dbf25babf71fdf580cf2f853b9d8a89661227f8d9a12  e2etests/tests/isis.go
f7492ddb95881bbcca902faa843592971fe0102b2fec5b4d2280ce377780e4a0  e2etests/tests/l3vpn_l2.go
70b904b1a6f8c1ba14128b76682b8037e014311b81cee477ad3ab29e84f0384e  e2etests/tests/l3vpn_routes.go
0c0d73bc68d8580f261c62028c80bb7b3cba3cecf3e1b56e8187f0c45ff44da9  e2etests/tests/mixed_l3vpn_l3vni.go
56dcc3246f8c44cc142ea351113a73213131f19f129b4973e41af2da99e3b56f  e2etests/tests/namespace_setup.go
a15b7a7d8c78bcf2b5822a29a40cec5a8a3af4e69d0e700ed84854b8564c33f3  e2etests/tests/node_status.go
94f6af591e906c1944fcef546b2b04302400131cfa7fb721e3d9a23d0e91ce42  e2etests/tests/passthrough_routes.go
7898ee328c546ff6d89bf2ed0a40df2920350e699d644072d1cdcbd6327b608c  e2etests/tests/password_secret.go
f341716479cdd8dfc099f8fdf0ad3631bc89674fd2fb2146b5df80d548662910  e2etests/tests/raw_config.go
39ea169b69fd2f982455bdb80858b45952f95e09dacb6b7a1032f2e45a03ee78  e2etests/tests/resiliency.go
6032035eab986bdf913c40e31add8633428dc8b35d021aac8582a1a4f9083a50  e2etests/tests/route_reflector.go
09f859c2ff447d60a21f132031d51c4f908e28b6d04aa556bd71f39cfaf810a2  e2etests/tests/route_target.go
db1eb2158f6ae80703c6ba4aaf39f559ea441e2fd8b9bd2ecf0894d78d772afc  e2etests/tests/routingdomain.go
37a65d9b34bd888f65bcaa857a3ba43bdbb256223bbb95b46973a6b3583a3e53  e2etests/tests/sessions.go
e267afa83a877037f06d26b81cacdde94e6dad7b6fb540dbac23d12f8c726010  e2etests/tests/singlesession.go
c796f295c01a4db4a62b613aac56dddf55634eb54b089a4440644fb4e6c145f1  e2etests/tests/static_and_api.go
4a19e0e3e391b7778943393cd7ce8e85680079a5cc5b96af10ef2800f63ff844  e2etests/tests/static_mirror.go
1a43458e9fc6d39e97d93c192dd22cc6e3ce2880a12e68521c3eeb2fa5f21d27  e2etests/tests/systemd_resiliency.go
06e6aaa749658824561598b5cc5e23876fb3aad7f46f6b6261b65fb40f021ff0  e2etests/tests/underlay_address_families.go
b204312ae6a9fa505097f38db3824e8ff30b834d46faa0c282b7bf4ca68f8e14  e2etests/tests/validate_pods.go
0450caed4f45c8e4bcd5654ffdbbe8db35355d98bd59e0f264f8c3380952a095  e2etests/tests/validate_routes.go
03a82a6834d642e23da356034c6262e80bf6bed2d922dcb452d820e53d813400  e2etests/tests/validate_session.go
1bc9e9c656c3ac94a3d810644b3599022d155170972cb5c1f57987b18490961e  e2etests/tests/webhooks.go
74b0702ea58701aff7accbe678e5024c6e93db473d6f474fb418c1c591ac12de  e2etests/tests/zebra_restart.go
ebd5f99ff2c69507476be1e6a64030bca9bd115f0d9ec19ac36a254026ecc059  go.mod
bae76c74123800b1ec9c4fc0252bcf9846241ca7191d1c0f5fb0e3fde78faa9b  go.sum
92a42fb7344bddf00441e7e874d105d8f2e0435fa6974c63cb9bf8f412a0978e  operator/PROJECT
bfccf448f72d353a51d34411972add846829cc816522d5bc13009a9d94039025  operator/api/v1alpha1/groupversion_info.go
08fbfff46f2e8b197e8fcdeb3504febd8d180a20a09ed8d01b223c45a67ac22e  operator/api/v1alpha1/openperouter_types.go
3c4fb7e57d3a9bb99f9715388cffb534912e2a8b1b9054df126cfe77842a519d  operator/api/v1alpha1/zz_generated.deepcopy.go
bb987e6a8ef45a99255d1d632812482d5f0f3255ea841520b4d90342df5fe2b3  operator/bindata/deployment/openperouter/.helmignore
a4cc31089b29b1f7ea243d1277d8ba68d2a597720f411e62e64e9a474e8b49fa  operator/bindata/deployment/openperouter/Chart.lock
bc950b777d39a979fa30ef09c6b6ef8fa160eb64c0bd2327d794e086207e6844  operator/bindata/deployment/openperouter/Chart.yaml
adcd242eb6b53eea3d8ae13a99b737e89151b87dabb38dedbe7ed7aac123fc0f  operator/bindata/deployment/openperouter/README.md
be1d436f1f9a4d4265568e1a6e2bff621703efa69d583c7dbb63de51daa05e5c  operator/bindata/deployment/openperouter/templates/NOTES.txt
9e5872f627fa2885b10db6b5b30bccf14efc9006c4712b561118e370fde50ff7  operator/bindata/deployment/openperouter/templates/_helpers.tpl
002a2313177b4f0be41a48be0890c57a133a2bf547bc9b7df695bf7dac4a9f8f  operator/bindata/deployment/openperouter/templates/controller.yaml
2c56f5862fa07e41c96d6e99b35e2582cbe82ab92035ea6d9945909999ae6ed5  operator/bindata/deployment/openperouter/templates/hostbridge.yaml
7b9d7beaaf17b0e3f6977a80bb985c67598560c77b0c5b2beda107df96238dc1  operator/bindata/deployment/openperouter/templates/nodemarker.yaml
42a5bd56773c6118b15af5be2237e095f4d8ffcdb85cf6e4750cee1b22b67238  operator/bindata/deployment/openperouter/templates/router.yaml
d97da8a3f61763e74f6777be19ddf2d0e5bda09801698dc01fafc7fcb05e08f1  operator/bindata/deployment/openperouter/templates/webhook.yaml
831a50b08ad0056802b100d7d776c9d6701914ce6a46af9ef88a2e0c42f0353b  operator/bindata/deployment/openperouter/values.schema.json
2ffc722a6d3cf9ad1ca022670e05486d90f1618af04afcbfbf819a941be99ddd  operator/bindata/deployment/openperouter/values.yaml
293466c242d73812cf501f0bf69948ead26187bc500a0dfd24617cfd69d81b79  operator/bundle/manifests/network.openperouter.io_l2vnis.yaml
ea2c47ff4b82d07ac46d3fb214f3fb891e2cd456c1f2797ab1d488e49c6beaef  operator/bundle/manifests/network.openperouter.io_l3passthroughs.yaml
92725907fde76d3775cabf0f5d8eb9132bb0b1c7b33cf0ef42312e6fa6639f5c  operator/bundle/manifests/network.openperouter.io_l3vnis.yaml
e64662b4d1f8e84ee3dc6b4762e49e1be80bd97ed7602cf4a41ed8d8cfd3b0da  operator/bundle/manifests/network.openperouter.io_l3vpns.yaml
5d59ab4bc7e7fe5399d3d7e3b725e70068f6544da14f1194af2bee956f5962d1  operator/bundle/manifests/network.openperouter.io_openperouters.yaml
db9daa03b4dd932a15e6aa8759afc9cf580d7ecc393358ea7665def315241528  operator/bundle/manifests/network.openperouter.io_rawfrrconfigs.yaml
b9a1b780dbfa0f2f2bf24d94f0290a0bdee1fa22e22003f3bfb87a0ac03f339a  operator/bundle/manifests/network.openperouter.io_routernodeconfigurationstatuses.yaml
194b884fadc528748c3f7b801437ff6808e39a39d797b86a31b291f5756e943c  operator/bundle/manifests/network.openperouter.io_underlays.yaml
9caf17c1f2604cbd56191ea6eb68b1c1eed69bec86a07a8fc8e0bf0ef36d6a10  operator/bundle/manifests/openpe-webhook-server-cert_v1_secret.yaml
6c3b35ed0193af2f4d953b5993ebaeced2404b5c3961d09d3efb92d3a33052e5  operator/bundle/manifests/openperouter-operator.clusterserviceversion.yaml
c28ca1f8922eff2ac0152fbc0dc1ec477a9e05f4c711e8c0747bb7de0050309a  operator/bundle/manifests/webhook-service_v1_service.yaml
7724aa77145d926e7d9dadb3b8282af37332025012240ffea5d57ee3cbe4db81  operator/bundle/metadata/annotations.yaml
44b6e4c00a1e7a364045e6493f5df2d8e895b3e2e7285564620a4efe1f815f41  operator/bundle.Dockerfile
43f9c077eb28c9e66f3567abe49a0c356ea06d4b96af13fb3d62c4707a3f49ab  operator/config/crd/bases/network.openperouter.io_openperouters.yaml
b6f00dcc5844157ce2760530453b05c75b184ba7c952624fc7fbd22e78314fcf  operator/config/crd/kustomization.yaml
b79f51a968507434d2e38ba95baef325cf024ac2356702751907ccdeb30f0cdf  operator/config/default/kustomization.yaml
30db217d2a8a7e4e31d0b80286472194fa751b42c23e211aa52e64fdcd28e7b0  operator/config/default/manager.yaml
0d961f08382c7f12ce2eaf241c171a40732fbcb4ec6cc28ffe18f61d9479632d  operator/config/manifests/bases/openperouter-operator.clusterserviceversion.yaml
04fadadbf8c7d4a49391e645724ee1b7d5deed79d84034a5c125534d92a8adfd  operator/config/network-policy/allow-metrics-traffic.yaml
12715f4867b5de2c784bd59493ee3ec3423910c8ed75d44b62f8830773f99355  operator/config/network-policy/kustomization.yaml
a4b1a7400199ed4f251ce82ca84c97322e777a0e9369f7f62a5b7686eeee2b48  operator/config/olm-install/install-resources.yaml
7c531c121a23986a5bcce97d4cdcfb406a40e2a963572ef8540c4e0eb66082be  operator/config/olm-install/kustomization.yaml
c0d5d174d544892095e2eff0baea8fe7ec1b9005974cd7d4eb14cbe9c0888711  operator/config/pods/env.yaml
075eea7c62ef0f61b29c2bbf69b8dfae384b2618234ac19e83cb923338486a77  operator/config/pods/kustomization.yaml
744a0c8c1ab18beea54a738b8bdfbc6c11df60798d8bd03b908974f671daaa0f  operator/config/pods/namespace.yaml
0263d69382ea49feff3d642e0596084359d0e22c2e669319c3edbb1fce254df7  operator/config/pods/operator.yaml
c7324b9d413208f085d47619d62622e7b43505a4cc4feff64d010d89b4253451  operator/config/prometheus/kustomization.yaml
6585875d184af2fc58b37c7858e811c49abb87d0c30f0a8f4653802b16329b90  operator/config/prometheus/monitor.yaml
181f2f128b701c6f4b1c0396b3ae1b082af99442b3b136789a02a7ea200c605d  operator/config/rbac/kustomization.yaml
80635a5f337dae55ab92738624f723b033c1cb4b04012bdf4e9e06d85efa3820  operator/config/rbac/role.yaml
85d5a13203dde5cea9797623c4bf978128aa14637dc99f53a3880f2eaad7a65b  operator/config/rbac/role_binding.yaml
e8c7069396ca373cf2213d7e72d419eeb7ec0f9b47f0b5867358f2e384d0ee21  operator/config/rbac/service_account.yaml
c91af07526b8370f5f886d57e8dc099d07bdd53566be2ec3a14138820ce811b0  operator/config/samples/kustomization.yaml
061a86f25df7fc4403230c6c270c3723efb849fa1be1374a3e8371a92c83cb89  operator/config/samples/openperouter.yaml
55382490c51bb5c2ed97c4b0fae204158d8a123b045da50cbb9806d71bcf1211  operator/config/samples/openperouter_grout.yaml
33df324e3ff1960c5a31c7cbfa70556fc8426ec86f2be893037005b06662a8e3  operator/config/webhook/backend/backend.yaml
10fb6180438001701c6cd78f17177b0ae778cdb30ff60de60ad83d54aa8d0586  operator/config/webhook/backend/kustomization.yaml
6ae59074d903dc3473c5fffff6211e19e70770c7ef53692abe4a77d072705858  operator/config/webhook/kustomization.yaml
7f8a047502dece45ec2d55b797114332965efa59187b5310930d1bd86794297f  operator/config/webhook/webhook/kustomization.yaml
fc0018e82114e63b83b6654d202ac706867234eb55d8216d244ef216658b3e4f  operator/config/webhook/webhook/kustomizeconfig.yaml
d597a9a5b3b853e6e529757dee417f2eb88eca6d962291fc2621a07122d3792d  operator/config/webhook/webhook/manifests.yaml
802390ee031f92ad66f98c7bc5bd1f0e33cac5669d409b8fb95ba3316d119db6  operator/config/webhook/webhook/secret.yaml
1eedf1c05aa8eedfe4f7172f80745929a454bf6ef06f0542e12887a17c7abe7f  operator/config/webhook/webhook/service.yaml
9b88d68473d64913411c7ca27edb5a104adb80b38c2fd94017fdc6402028e251  operator/internal/envconfig/envconfig.go
919893db52b06d4730a35928e69203ebca764dfdd16eddbc2da7351238f04856  operator/internal/envconfig/envconfig_test.go
29db95f515d900e30bf46137e3d6f8754d0d7df6e5592e43ea1b490a36b71203  operator/internal/helm/apply.go
cf5bf36f45a697a21a622ecbee5cb5db2ec7c15ef620bb3d10b41ff21b13d2b2  operator/internal/helm/chart.go
a4d0ec7c2c8f2e919502984bbcbc4f94e3cbd9ffcff7c539f848ac3b72ad87ac  operator/internal/helm/helm.go
c7d133f7a6ac3bfeae5d1f3ee044d436ae25a89f576c10e19ad09458e4a7ad0d  operator/internal/helm/helm_test.go
091fb3d0fc4658e9a4f6cf922d32e0e8a52d4a377b6a4bc7b6d753eafef12138  operator/internal/helm/merge.go
2ea09c0ec1af858325b014ed51651311cf17be7c48fee8c8d4f0a88436acf1ec  operator/internal/helm/merge_test.go
e26825eb5a4bf671010e4c164dcde1b7e4383e6842ad5344562c78dbcffd5ee3  operator/internal/openperouter_controller.go
a3cf516e650608b1c50958440b5325e2fb4e036c749e80f5147914ea1cc8856a  operator/internal/openperouter_controller_test.go
375de6ea824866410195c3afb41a06c15ce04d63300cbfc41db665ba978c7fb5  operator/internal/suite_test.go
f1a1a2a021e5900ba05106279924be6085c472ed3b45ac6a4edaaa33dc917fcd  operator/main.go
```

## Execution evidence

Implementation in progress. Runtime capability and suite success are not yet established.

## Implementation progress

Two developer agents work on separate VXLAN and bridge/gateway files; the coordinator integrates VNI/controller lifecycle, with an independent reviewer sending findings directly to developers. The reviewer identified unexpected VLAN memberships surviving reconciliation; this was corrected with a drift regression. Static review found no remaining blockers in the first integration.

Early checks:

- `go test ./internal/hostnetwork ./internal/controller/routerconfiguration`: passed.
- Focused wire-format tests and privileged-suite compilation: passed.
- `sudo -n /tmp/openperouter-vlan.test -ginkgo.focus='VLAN-aware VNI endpoints|VLAN-aware anycast gateways'`: 3 passed, exercising actual kernel bridge/SVI/macvlan/FDB operations. Initial FDB-delete test required `NTF_SELF` on deletion of a dumped local entry; corrected and rerun successfully.
- Broader host-network container/OVS harness, `make test`, and `make lint`: in progress; final results below will supersede intermediate runs.

The installed Go compiler is 1.26.4, the running Linux kernel is 6.19.10-300.fc44.x86_64, and the unchanged Dockerfile selects FRR 10.6.0. These identities are prerequisites, not evidence that an unexecuted deployment lane passes.

Existing CI filters remain authoritative: pod `!systemdmode && !grout-only`; host `!grout-only && !podmode-only` with the existing three test-name skips; Grout `grout-support || grout-only`. Public/e2e baseline fingerprints above are retained for final comparison.

### Privileged integration checkpoint

`go test -tags=runasroot -c -race -o bin/hostnetwork.test ./internal/hostnetwork` followed by the documented Docker/OVS harness passed **76/76 specs, zero skipped**, with the race detector enabled. Log: `/tmp/openperouter-hostnetwork-race.log`. The earlier non-race harness passed 74/74 before the last two drift/no-op regressions were added (`/tmp/openperouter-hostnetwork-harness.log`). This proves actual kernel operations, Linux and OVS attachments, topology/MTU assertions, mapping/filter repairs, legacy conversion and cleanup. Traffic dispatch and full FRR behavior remain e2e gates.

The OVS tests required no file edits: shared `vni_test.go` topology assertions are reused by `vni_ovs_bridge_test.go`, preserving its existing managed/external/shared bridge ownership cases. The production neighbor-owner regression lives in new `bridgerefresh/vlan_test.go`; existing bridge-based refresher fixtures remain useful for the independent Grout path.

An intermediate audit checked all **222 recorded baseline SHA256 entries**: none changed. Final audit must be repeated after all deployment/packaging commands.

### Review and lint closure

`make lint` initially reported nine findings (two unchecked namespace closes, two complexity thresholds, three slice-preallocation suggestions, an unused bridge option and a dead bridge helper). These were corrected without suppression. The reviewer checked the resulting legacy-preflight extraction, grouped deletion ordering and removed helpers. `make lint` rerun passed with **0 issues** (`/tmp/openperouter-lint-final.log`).

The namespace-reconstruction regression initially raced asynchronous kernel cleanup: the host veth remained for approximately 44 ms after removing the namespace bind mount. It now waits for actual host-peer disappearance before creating the replacement namespace; the unchanged provisioning sequence then passes. The focused kernel run is recorded in `/tmp/openperouter-retry-probe.log`; the final full privileged run will include it.

Final kernel image build passed: `quay.io/openperouter/router:main`, image ID `sha256:448a58fe08676a7951019110db3dfbd371706705f8cd2e0d13f5444da8acd149`. Deployment into a fresh pod-mode environment has started; no e2e result is claimed yet.

### Required local gate: passed

`make test` exited 0. All ordinary packages passed, including controller status/resource-error coverage, conversion, API/static configuration, FRR rendering against the real image, and Grout. Its privileged OVS-enabled, race-enabled harness passed **78/78 host-network specs**, **22/22 bridge-refresher specs**, **12/12 sysctl specs**, and the Grout/netnamespace test binaries, with zero skips in the Ginkgo suites. Log: `/tmp/openperouter-make-test.log`.

Fresh-cluster deployment initially failed before router provisioning because stale `leaf1`/`leaf2` bridge ports belonged to an old ContainerLab topology. Its six-day-old `check_veths` monitor retained sockets into removed container namespaces. Stopping that exact old monitor released the interfaces; the attempted explicit port deletes found them already gone. Deployment is retrying with no product-code workaround. Logs: `/tmp/openperouter-deploy.log`, `/tmp/openperouter-deploy-retry.log`.

### US1 disconnected compatibility checkpoint: passed

Fresh pod-mode deployment passed (`make docker-build`, then `make deploy`; deployment retry log above). Mode detection used the absent hostbridge daemonset and the installed **controller daemonset**'s arguments (this repository installs a daemonset, rather than the deployment kind assumed by the generic skill prose).

```sh
KUBECONFIG=bin/kubeconfig make e2etests \
  KIND_EXPORT_LOGS=/tmp/openperouter-e2e-us1 \
  GINKGO_ARGS="--label-filter='!systemdmode && !grout-only' --focus='Disconnected L2VNI east/west traffic'"
```

Result: **3 passed, 0 failed, 183 excluded by focus/mode**, 78.606 seconds of suite execution. JSON report: `/tmp/openperouter-e2e-us1/e2e-report.json`; log: `/tmp/openperouter-e2e-us1.log`. Unchanged scenarios verified disconnected bidirectional connectivity, configured RD advertisement and route-target retention.

Packet capture `/tmp/openperouter-captures/us1-host.pcap` contains 35 Ethernet frames on unchanged `host-e-300`: 30 IPv6, 3 ARP and 2 IPv4, with **zero 802.1Q/802.1ad headers**. Diagnostic capture tooling was built in a separate temporary Alpine image; no product image/dependency/e2e changes were made.


### Same-port packet proof and routed investigation

The focused routed run uses the normal pod-mode filter and `--focus-file='evpn_l2.go|evpn_l2vnis_with_same_l3vni.go|l3vpn_l2.go|mixed_l3vpn_l3vni.go|route_target.go|ipv6_vtep.go|underlay_address_families.go'`, with report root `/tmp/openperouter-e2e-routing` and log `/tmp/openperouter-e2e-routing.log`. It is still running; no US2/full-suite pass is claimed.

The nonempty host-wide underlay capture `/tmp/openperouter-captures/routed-all.pcap` contains **1,000 UDP-4789 VXLAN packets**: VNI 300 (18), L2 VNI 110 (562), L3 VNI 100 (420). Inner Ethernet types are IPv6 (554), IPv4 (434), ARP (12), with **zero 802.1Q/802.1ad inner headers**. Successful unchanged IPv4/IPv6/dual-stack routed Linux/OVS scenarios complement the earlier privileged state checks: same-port L2/L3 dispatch and independent internal VLAN reuse now have actual traffic evidence (T008). This traffic proof was obtained after story integration; the initial privileged state-only run did not establish traffic dispatch by itself.

One SRv6 gateway/no-SNAT IPv4 case failed at `e2etests/tests/l3vpn_l2.go:375`; the gateway-free/SNAT case passed. Leaf and router dumps show the pod subnet is advertised and installed. Captured SYN-ACKs return over VNI 110, while kernel diagnostics report `ip_tunnel_core: Dead loop on virtual device vni110`. Investigation is ongoing with the reviewer and both developers; no test assertion, deadline or filter is being relaxed.
