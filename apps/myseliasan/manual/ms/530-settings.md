---
title: Penyunting tetapan
category: admin
categoryLabel: Pentadbiran
summary: Sunting konfigurasi satah kawalan dalam aplikasi — dan sebab mula semula ialah sebahagian daripadanya.
order: 530
---

# Penyunting tetapan

**Settings** menyunting konfigurasi satah kawalan itu sendiri dari dalam aplikasi. Perubahan ditulis
ke `config.json` dan berkuat kuasa **selepas mula semula**.

Hanya **superadmin** boleh melihat atau mengubahnya, dan itu bukan peraturan matriks yang boleh anda
wakilkan: nilai-nilai ini menentukan cara satah kawalan mengesahkan orang dan mencapai armada anda.

## Simpan, kemudian mula semula {#restart}

Menyimpan merekodkan perubahan; ia tidak menerapkannya. Halaman itu kemudian memberitahu anda mula
semula diperlukan dan menawarkan **Restart now**.

Pembahagian ini disengajakan. Suntingan konfigurasi yang berkuat kuasa di tengah-tengah permintaan
akan mengubah peraturan di bawah sesi yang sedang berjalan — versi yang jujur ialah menulis fail
itu, menyatakannya dengan jelas, dan mula semula apabila anda bersedia. Ia juga bermakna anda boleh
membuat beberapa perubahan berkaitan dan hanya membayar satu kali mula semula.

Jangkakan aplikasi tidak tersedia sebentar apabila anda melakukannya.

## Apa yang boleh anda sunting di sini {#sections}

| Bahagian | Meliputi |
|---|---|
| **Local Login** | Akaun pentadbir terbina dalam yang digunakan apabila log masuk tunggal tidak tersedia. |
| **Single Sign-On** | Log masuk bersekutu melalui pembekal identiti myidsan. |
| **Connectivity** | Penemuan nod, pengambilan, dan port yang digunakan armada untuk mencapai satah kawalan ini. |
| **Security** | Token, sijil TLS, dasar keselamatan kandungan, had kadar API. |
| **Storage & Cache** | Storan fail yang dimuat naik, pembersihannya, dan bahagian belakang cache. |
| **Logging & Telemetry** | Pengelogan aplikasi dan API, pengekalannya, dan titik akhir metrik. |
| **AI Agent** | Ringkasan harian dan model bahasa pilihan — lihat [Menyediakan model bahasa](language-model). |
| **System** | Versi yang sedang berjalan dan kawalan proses. |

## Apa yang tidak boleh anda sunting di sini, dengan sengaja {#not-here}

Ini ialah **subset selamat** bagi konfigurasi. Sambungan pangkalan data, tetapan pendengaran
pelayan, dan kelakuan bootstrap tidak boleh disunting daripada aplikasi.

Itu had yang disengajakan dan bukan terlepas pandang: kesilapan taip pada tetapan pangkalan data
yang disimpan melalui aplikasi yang memerlukan pangkalan data itu untuk berjalan akan meninggalkan
anda dengan peranti yang tidak boleh dimulakan dan tiada skrin untuk membaikinya. Nilai-nilai itu
kekal dalam fail, di mana membaikinya tidak bergantung pada aplikasi yang sihat.

## Rahsia {#secrets}

Medan rahsia yang dibiarkan **kosong akan mengekalkan nilai semasanya**; ia tidak dikosongkan.

Rahsia yang disimpan tidak pernah dipaparkan kembali kepada anda, jadi kosong bermakna "tidak
berubah" dan bukan "kosong" — dan itulah yang membolehkan anda menyunting medan bersebelahan tanpa
menaip semula kunci yang mungkin tiada pada anda ketika itu.

## Pulihkan nilai lalai {#defaults}

Setiap bahagian boleh diset semula kepada nilai asalnya. Itu masih memerlukan mula semula untuk
berkuat kuasa, dan ia tindakan setiap bahagian dan bukan set semula keseluruhan konfigurasi.

Ia langkah yang betul apabila sesuatu bahagian telah ditala sehingga ke keadaan yang tiada siapa
boleh jelaskan — mulakan daripada nilai yang dihantar dan ubah satu perkara pada satu masa.

## Jika satah kawalan tidak mahu bermula selepas sesuatu perubahan {#recovery}

Aplikasi yang tidak mahu bermula selepas perubahan tetapan hampir selalunya disebabkan nilai yang
salah dalam fail itu, dan fail itulah tempat anda membetulkannya. Konfigurasi yang disunting di sini
mendarat dalam `config.json` di dalam direktori data aplikasi.

Jika sesuatu perubahan menghentikan satah kawalan daripada bermula, fail itulah laluan pemulihannya:
betulkan atau buang nilai yang bermasalah pada cakera dan mulakannya semula. Penyunting ini ialah
kemudahan di atas sebuah fail yang kekal boleh dibaca dan diperbaiki dengan tangan, dan itulah sifat
yang anda mahukan pada hari kemudahan itu tidak tersedia.
