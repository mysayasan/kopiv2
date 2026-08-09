---
title: Kemas kini, mula semula dan semakan kesihatan
category: administration
categoryLabel: Pentadbiran
summary: Kekalkan peranti terkini, mulakannya semula dengan selamat, dan baca apa yang dinyatakan titik akhir kesihatan.
order: 550
---

# Kemas kini, mula semula dan semakan kesihatan

**Tetapan → Versi & Kesihatan** meliputi versi yang berjalan, kemas kini, mula semula, kebergantungan
runtime dan kesihatan perkhidmatan.

## Versi yang berjalan {#version}

Versi, versi teras kongsi, commit dan tarikh binaan ditunjukkan di sini dan pada pengaki halaman.

Nyatakan set penuh apabila melaporkan masalah. "Yang terbaharu" bukan versi, dan perbezaan antara dua
binaan bagi nombor versi yang sama ialah tepat maklumat yang mengenal pasti sesuatu regresi.

## Mengemas kini {#updating}

Cara anda mengemas kini bergantung pada cara peranti dipasang, dan tab memberitahu anda kes mana anda
berada:

- **Kemas kini dalam aplikasi.** Kemas kini ditawarkan dan digunakan dari sini.
- **Diuruskan oleh pengurus pakej.** Kemas kini dengan alat platform anda —
  `sudo apt update && sudo apt install --only-upgrade mymatasan` atau setara `dnf`. Kemas kini dalam
  aplikasi sengaja tidak tersedia supaya pengurus pakej kekal sebagai sumber kebenaran.
- **Bekas kontena.** Tarik imej baharu dan cipta semula kontena.

Sebelum sebarang kemas kini:

1. **Ambil [sandaran konfigurasi](backup-and-restore).**
2. **Sahkan [kunci pemulihan](encryption-at-rest#export) anda telah dieksport dan disahkan.**
3. Kemas kini apabila ada orang yang boleh memerhatikannya, bukan pada petang Jumaat.

Kemas kini memulakan semula peranti. Paparan langsung terputus, rakaman berhenti untuk mula semula,
dan sebarang pengesanan yang sedang berjalan hilang. Pada tapak yang sibuk itu seminit rakaman yang
hilang — jadualkannya sewajarnya.

## Memulakan semula {#restart}

**Mulakan semula aplikasi** memulakan semula dengan bersih: perakam dihentikan dengan betul supaya
segmen diselesaikan dan bukan terpotong.

Mulakan semula apabila sesuatu tetapan dilabelkan *(mula semula untuk berkuat kuasa)* — ffmpeg yang
baru dipasang ialah kes biasa — dan apabila peranti berkelakuan pelik dengan cara yang tidak boleh
anda terangkan. Ia selamat, dan ia langkah diagnostik pertama yang sah.

Sentiasa mulakan semula dari sini dan bukan membunuh proses itu. Hentian mengejut boleh meninggalkan
segmen yang sedang berjalan tidak selesai.

## Kebergantungan runtime {#dependencies}

Tab melaporkan sama ada perkara yang disandari peranti hadir:

- **ffmpeg** — paparan langsung, rakaman dan laluan tangkapan AI. Tiada apa berfungsi tanpanya.
- **Runtime AI** — Python berserta pustaka pengesanan. Tanpanya, langsung tiada pengesanan.
- **Kebergantungan OCR** — diperlukan khusus untuk [plat nombor](fire-smoke-and-plates#lpr).

Setiap satu boleh dipasang dari dalam aplikasi. Semak tab ini dahulu setiap kali keseluruhan
keupayaan nampaknya tiada dan bukan sekadar tersalah konfigurasi — "pengesanan tidak pernah berfungsi
pada pemasangan ini" biasanya runtime yang hilang, bukan peraturan yang buruk.

## Liveness dan readiness {#health}

Dua isyarat kesihatan, dengan maksud berbeza:

- **Liveness** — proses itu bertindak balas. Penyelia menggunakannya untuk memutuskan sama ada
  hendak memulakan semula peranti.
- **Readiness** — tambahan pula, pangkalan data dan cache boleh dicapai. Pengimbang beban
  menggunakannya untuk memutuskan sama ada hendak menghantar trafik.

Pemantau aplikasi itu sendiri — kesihatan mesin, kesihatan kamera — muncul sebagai medan readiness
**nasihat**. Ia tidak pernah menyekat readiness, kerana kamera yang luar talian bukan sebab untuk
mengeluarkan perakam daripada perkhidmatan. Nilai yang merosot masih wajar disiasat; cuma ia bukan
gangguan perkhidmatan.

Jika anda berintegrasi dengan pemantauan luaran, perhatikan liveness untuk "adakah ia hidup",
readiness untuk "bolehkah ia berkhidmat", dan suapan pemberitahuan untuk "adakah ada yang tidak kena"
— yang ketiga menangkap apa yang dua yang pertama direka untuk tidak menangkap.
