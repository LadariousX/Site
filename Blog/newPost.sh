echo "post slug:"
read slug
mkdir -p posts/"$slug"/images
echo "---\ntitle: ''\nslug: $slug\ndate: YEAR-MO-DY\nstatus: published\nexcerpt: ''\ncover: 000.jpg\nrepo: ''\n---" > posts/"$slug"/"$slug".md
echo "post template \""$slug"\" created"